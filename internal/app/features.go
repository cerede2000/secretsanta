package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"secretsanta/internal/access/magic"
	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/draw"
	"secretsanta/internal/enrolment"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/letter"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildkim"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/ssrf"
)

// features liste les fonctionnalités branchées : UNE ligne par module, dans
// l'ordre des dépendances (le port DrawState avant participants, le bus avant
// groupsettings, le service de jetons avant l'inscription et les pages).
// Adaptateurs encore temporaires : voir adapters.go (inscriptions en attente,
// réparation du tirage) et mailTransport (envoi SMTP).
var features = []func(*App) error{
	registerBus,
	registerGroups,
	registerDrawState,
	registerParticipants,
	registerGroupSettings,
	registerRules,
	registerDraw,
	registerRuleChange,
	registerTokens,
	registerEnrolment,
	registerMail,
	registerMailQueue,
	registerMagic,
	registerImages,
	registerLetter,
	registerWeb,
	registerGroupPages,
	registerAccessPages,
}

// publicBase renvoie PUBLIC_URL sans barre finale.
func (a *App) publicBase() string { return strings.TrimRight(a.Cfg.PublicURL, "/") }

// registerBus crée le bus d'événements en mémoire.
func registerBus(a *App) error {
	if a.Bus == nil {
		a.Bus = &ports.MemoryEventBus{}
	}
	return nil
}

// registerGroups construit le service des groupes (routes métier à venir).
func registerGroups(a *App) error {
	s, err := groups.NewService(a.DB, a.Clock, a.Cfg.InstancePass)
	if err != nil {
		return err
	}
	a.Groups = s
	return nil
}

// registerDrawState construit le port DrawState réel ; il ne dépend que de la
// base, donc il précède participants (qui en a besoin).
func registerDrawState(a *App) error {
	a.DrawState = draw.NewState(a.DB)
	return nil
}

// registerParticipants construit le service des participants.
func registerParticipants(a *App) error {
	a.Participants = participants.NewService(a.DB, a.Clock, a.DrawState)
	return nil
}

// registerGroupSettings construit les réglages de groupe ; les langues
// acceptées sont celles du catalogue.
func registerGroupSettings(a *App) error {
	a.GroupSettings = groupsettings.NewService(a.DB, a.Clock, a.Bus, a.DrawState, a.Cat)
	return nil
}

// registerRules construit le service des règles, avec le rôle de tirage lu
// via participants (participantDirectory).
func registerRules(a *App) error {
	a.Rules = rules.NewService(a.DB, participantDirectory{svc: a.Participants})
	return nil
}

// registerDraw construit le service du tirage. Les inscriptions en attente
// viennent de l'adaptateur temporaire noPendingRegistrations ; la porte de
// révélation est celle par défaut (date de révélation du groupe).
func registerDraw(a *App) error {
	a.Draw = draw.NewService(a.DB, a.Clock, a.Groups, a.Participants, a.Rules,
		noPendingRegistrations{}, draw.Options{Location: a.Cfg.Location})
	return nil
}

// registerRuleChange construit le service des modifications de règles, avec
// l'adaptateur temporaire previewOnlyRepair.
func registerRuleChange(a *App) error {
	a.RuleChange = rulechange.NewService(a.DB, a.Clock, a.DrawState, previewOnlyRepair{db: a.DB})
	return nil
}

// registerEnrolment construit la création vérifiée d'un participant ; le lien
// personnel est PUBLIC_URL/<slug>/acces/<jeton>.
func registerEnrolment(a *App) error {
	base := a.publicBase()
	a.Enrolment = enrolment.NewService(a.DB, a.Participants, a.Tokens, func(slug, tok string) string {
		return base + "/" + slug + "/acces/" + tok
	})
	return nil
}

// registerTokens construit le service des accès participant : empreintes à clé
// (SIGNING_SECRET), magasin SQL.
func registerTokens(a *App) error {
	s, err := token.NewService(sqlstore.New(a.DB), token.LoginCodeLength, []byte(a.Cfg.SigningKey.Reveal()))
	if err != nil {
		return fmt.Errorf("accès participant : %w", err)
	}
	a.Tokens = s
	return nil
}

// registerMail construit le signeur DKIM (désactivé sans configuration) et le
// constructeur de messages. Sans adresse d'expédition configurée (envoi
// désactivé), Mail reste nil.
func registerMail(a *App) error {
	signer, err := maildkim.New(a.Cfg.DKIM)
	if err != nil {
		return fmt.Errorf("signature DKIM : %w", err)
	}
	a.DKIM = signer
	if a.Cfg.SMTP.From == "" {
		return nil
	}
	b, err := mailmsg.New(mailmsg.OptionsFromConfig(&a.Cfg, a.Clock, nil))
	if err != nil {
		return fmt.Errorf("messages courriel : %w", err)
	}
	a.Mail = b
	return nil
}

// mailTransport fournit le transport d'envoi de la file. POINT D'ANCRAGE : la
// fonctionnalité d'envoi SMTP remplace cette fonction (ou l'affecte, en test)
// par une qui renvoie un queue.Transport construit sur la configuration SMTP.
// Tant qu'elle renvoie nil, la file ne démarre pas et l'envoi est désactivé :
// mailOutbox refuse tout message (ErrMailDisabled) au lieu de l'empiler.
var mailTransport = func(*App) (queue.Transport, error) { return nil, nil }

// registerMailQueue construit la file d'envoi quand un constructeur de messages
// existe (adresse d'expédition configurée) ; elle ne tourne que si
// mailTransport fournit un transport. Elle s'arrête avec l'application.
func registerMailQueue(a *App) error {
	tr, err := mailTransport(a)
	if err != nil {
		return fmt.Errorf("transport courriel : %w", err)
	}
	a.mail = mailOutbox{}
	if a.Mail == nil {
		return nil
	}
	q, err := queue.New(queue.Options{
		DB: a.DB, Clock: a.Clock, Builder: a.Mail, Transport: tr, Signer: a.DKIM,
		MaxPerMinute: a.Cfg.Mail.MaxPerMinute,
	})
	if err != nil {
		return fmt.Errorf("file d'envoi : %w", err)
	}
	a.MailQueue = q
	if tr == nil {
		return nil
	}
	a.mail = mailOutbox{q: q}
	a.OnStart(func(ctx context.Context) { q.Run(ctx, mailTickEvery) })
	return nil
}

// registerMagic construit le lien magique : recherche par adresse au-dessus de
// participants, lien PUBLIC_URL/<slug>/connexion/lien/<jeton>, courriel du
// modèle MagicLink, envoi par la file. Les traitements d'arrière-plan sont
// attendus à l'arrêt et les liens expirés purgés périodiquement.
func registerMagic(a *App) error {
	dir := participantEmails{svc: a.Participants}
	base := a.publicBase()
	s, err := magic.New(magic.Config{
		DB: a.DB, Clock: a.Clock, Directory: dir, Sender: a.mail,
		Link: func(slug, tok string) string { return base + "/" + slug + "/connexion/lien/" + tok },
		Compose: magicComposer{
			cat: a.Cat, groups: a.Groups, settings: a.GroupSettings, people: dir, base: base,
		}.compose,
	})
	if err != nil {
		return fmt.Errorf("lien magique : %w", err)
	}
	a.Magic = s
	a.Every("magic.purge_links", purgeInterval, func(ctx context.Context) error {
		_, err := s.PurgeExpired(ctx)
		return err
	})
	a.OnClose(s.Wait)
	return nil
}

// registerImages construit la garde SSRF et le magasin des copies locales
// d'images (DATA_DIR/images, créé au besoin). La route /images/ est montée par
// registerWeb ; aucune route de recherche encore.
func registerImages(a *App) error {
	g, err := ssrf.New(ssrf.Options{SearxngURL: a.Cfg.Sources.SearxngURL})
	if err != nil {
		return fmt.Errorf("garde SSRF : %w", err)
	}
	st, err := ssrf.NewStore(filepath.Join(a.Cfg.DataDir, "images"), "/images")
	if err != nil {
		return fmt.Errorf("magasin d'images : %w", err)
	}
	a.SSRF, a.Images = g, st
	return nil
}

// registerLetter construit le service des lettres (bus en mémoire, images dans
// DATA_DIR/letter-images). Aucune route : les pages viennent plus tard.
func registerLetter(a *App) error {
	a.Letter = letter.NewService(a.DB, a.Clock, a.Bus, filepath.Join(a.Cfg.DataDir, "letter-images"))
	return nil
}
