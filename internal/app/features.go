package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"secretsanta/internal/access/magic"
	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/draw"
	"secretsanta/internal/enrich"
	"secretsanta/internal/enrolment"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/letter"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/transport"
	"secretsanta/internal/maildiag"
	"secretsanta/internal/maildkim"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/sources"
	"secretsanta/internal/ssrf"
)

// features liste les fonctionnalités branchées : UNE ligne par module, dans
// l'ordre des dépendances (le port DrawState avant participants, le bus avant
// groupsettings, le service de jetons avant l'inscription et les pages).
var features = []func(*App) error{
	registerBus,
	registerGroups,
	registerDrawState,
	registerParticipants,
	registerGroupSettings,
	registerRules,
	// Les jetons, l'enrôlement et la file d'envoi précèdent l'inscription par
	// lien (registerRegistration), qui tient le port RegistrationState du
	// tirage : elle précède donc registerDraw.
	registerTokens,
	registerEnrolment,
	registerMail,
	registerSuppression,
	registerMailQueue,
	registerRegistration,
	registerDraw,
	registerMailDiag,
	registerReminderRules,
	registerMailUnsub,
	registerReveal,
	// Les relances suivent la désinscription, la suppression et la file ; le
	// moteur n'est pas bâti sans file d'envoi.
	registerReminders,
	registerBudgetNotice,
	registerMagic,
	registerImages,
	registerSuggestions,
	registerLetter,
	registerLetterRead,
	registerSuggestionService,
	// La réparation du tirage archive les lettres (letterread) et prévient la
	// révélation : elle suit registerLetterRead et registerReveal.
	registerRuleChange,
	registerAdjust,
	registerWeb,
	registerGroupPages,
	registerAccessPages,
	registerRevealPages,
	registerTargetPage,
	registerRegistrationPages,
	registerAdminPages,
	// Les pages de la vague 9 suivent registerAdminPages : elles ont besoin du
	// registre de navigation, du journal et du tirage avec sa révélation.
	registerLetterPages,
	registerMailboxPage,
	registerRegistrationAdmin,
	registerDrawPages,
	registerPeoplePages,
	// Les pages de la vague 10 suivent registerAdminPages (registre de
	// navigation, journal).
	registerAdminConsult,
	registerAdminOps,
	// Les pages et services de la vague 11 : la page de délivrabilité suit
	// registerAdminPages (navigation) ; la page des suggestions suit
	// registerSuggestionService ; la notification du tireur suit registerLetter
	// et registerReminders.
	registerMailDiagPages,
	registerSuggestionPages,
	registerGiverNotify,
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

// registerDraw construit le service du tirage ; les options du groupe (exclure
// l'année précédente, anti-échange) viennent de groupsettings. Les inscriptions en attente
// viennent du service de vérification (verify.Service, port RegistrationState :
// compteur réel hors inscriptions ignorées) ; la porte de
// révélation est celle du paquet reveal (ouverture enregistrée, absence de
// date ou date atteinte). Elle ne dépend que de la base et de l'horloge, donc
// elle précède le service de révélation, qui a besoin du service du tirage.
func registerDraw(a *App) error {
	a.Draw = draw.NewService(a.DB, a.Clock, a.Groups, a.Participants, a.Rules,
		a.Verify, draw.Options{Location: a.Cfg.Location, Gate: reveal.NewGate(a.DB, a.Clock), Settings: a.GroupSettings})
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

// mailTransport fournit le transport d'envoi de la file : le transport SMTP
// construit sur la configuration (SMTP_HOST...). Sans SMTP_HOST il renvoie nil,
// la file ne démarre pas et l'envoi est désactivé : mailOutbox refuse tout
// message (ErrMailDisabled) au lieu de l'empiler. Variable de paquet pour que
// les tests y mettent un faux transport. La signature DKIM est faite par la
// file (a.DKIM), pas ici.
var mailTransport = func(a *App) (queue.Transport, error) {
	return transport.FromConfig(&a.Cfg, nil, a.Clock)
}

// registerSuppression construit la liste des adresses refusées définitivement
// (SMTP 5xx), par groupe ; elle précède la file d'envoi qui la consulte.
func registerSuppression(a *App) error {
	a.Suppress = mailsuppress.New(a.DB, a.Clock)
	return nil
}

// registerMailDiag construit le diagnostic de délivrabilité (DNS, SPF, DKIM,
// DMARC, TLS) sur la configuration ; la page est celle de registerMailDiagPages.
func registerMailDiag(a *App) error {
	o := maildiag.Options{Config: &a.Cfg, Clock: a.Clock}
	if mailDiagOptions != nil {
		mailDiagOptions(&o)
	}
	a.MailDiag = maildiag.New(o)
	return nil
}

// mailDiagOptions ajuste les options du diagnostic (nil en production) : les
// tests y mettent un résolveur DNS sans réseau.
var mailDiagOptions func(*maildiag.Options)

// registerReminderRules construit les règles de relance (fenêtre d'envoi dans
// le fuseau APP_TIMEZONE) ; aucune route encore.
func registerReminderRules(a *App) error {
	a.ReminderRules = reminderrules.NewService(a.DB, a.Clock, a.Cfg.Location)
	return nil
}

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
		MaxPerMinute: a.Cfg.Mail.MaxPerMinute, Suppression: a.Suppress,
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

// registerMailUnsub construit le service de désinscription par type de courriel
// (liens signés par SIGNING_SECRET, base PUBLIC_URL) et monte ses routes
// GET/POST /desinscription/{jeton} sur Mux. Le POST « un clic » n'a pas de
// jeton CSRF : la chaîne l'exempte par mailunsub.IsOneClickPath (voir New), la
// signature du jeton tenant lieu de preuve.
func registerMailUnsub(a *App) error {
	s, err := mailunsub.NewService(mailunsub.Options{
		DB: a.DB, Clock: a.Clock, SigningKey: []byte(a.Cfg.SigningKey.Reveal()), PublicURL: a.Cfg.PublicURL,
	})
	if err != nil {
		return fmt.Errorf("désinscription : %w", err)
	}
	p, err := mailunsub.New(mailunsub.PageOptions{Service: s, Groups: a.Groups, Translator: a.Cat})
	if err != nil {
		return fmt.Errorf("pages de désinscription : %w", err)
	}
	p.Register(a.Mux)
	a.Unsub = s
	return nil
}

// registerSuggestions construit la chaîne de sources de suggestions et
// l'enrichissement des liens Amazon. Les deux passent par le client de la garde
// SSRF (ssrf.Guard.Client, bâti par outbound.New) : aucun appel sortant ne
// contourne la garde. Services seulement, aucune page : elle viendra avec la
// lettre. Elle doit suivre registerImages (garde et magasin d'images).
func registerSuggestions(a *App) error {
	a.Sources = sources.FromConfig(a.Cfg.Sources, a.SSRF.Client(), slog.Default())
	a.Enrich = &enrich.Amazon{Fetcher: a.SSRF, Images: a.Images, Logger: slog.Default()}
	return nil
}
