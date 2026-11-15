package app

import (
	"fmt"
	"path/filepath"

	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/groups"
	"secretsanta/internal/letter"
	"secretsanta/internal/maildkim"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/ssrf"
)

// features liste les fonctionnalités branchées : UNE ligne par module.
// Pas encore branchés (leurs ports ou leurs phases arrivent plus tard) :
// access/magic (Directory, LinkFunc, Composer, ports manquants). Aucune route
// métier n'est encore enregistrée par ces fonctions.
var features = []func(*App) error{
	registerGroups,
	registerParticipants,
	registerRules,
	registerTokens,
	registerMail,
	registerImages,
	registerLetter,
	registerWeb,
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

// registerParticipants construit le service des participants. Le port
// DrawState réel (vague du tirage, F4) n'existe pas encore : l'adaptateur
// temporaire drawNotDone le remplace.
func registerParticipants(a *App) error {
	a.Participants = participants.NewService(a.DB, a.Clock, drawNotDone{})
	return nil
}

// registerRules construit le service des règles, avec le rôle de tirage lu
// via participants (participantDirectory).
func registerRules(a *App) error {
	a.Rules = rules.NewService(a.DB, participantDirectory{svc: a.Participants})
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
	if a.Bus == nil {
		a.Bus = &ports.MemoryEventBus{}
	}
	a.Letter = letter.NewService(a.DB, a.Clock, a.Bus, filepath.Join(a.Cfg.DataDir, "letter-images"))
	return nil
}
