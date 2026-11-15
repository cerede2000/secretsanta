package app

import (
	"context"
	"fmt"
	"strings"

	"secretsanta/internal/admin/drawpages"
	"secretsanta/internal/admin/people"
	"secretsanta/internal/draw"
	"secretsanta/internal/letterpages"
	"secretsanta/internal/mailboxpage"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/invite"
	regadmin "secretsanta/internal/registrationpages/admin"
	"secretsanta/internal/shared/errs"
)

// Chemins des pages voisines que les pages de la vague 9 s'envoient les unes
// aux autres. Ils sont écrits ici, à un seul endroit : un test
// (TestDefaultInternalLinksReachARoute) vérifie que chaque lien émis monte sur
// une route existante ou sur une page connue « à venir ».
const (
	mailboxPath = "/{slug}/boite"
)

func slugPath(pattern, slug string) string { return strings.ReplaceAll(pattern, "{slug}", slug) }

// letterNames adapte participants.Service au port letterpages.Names : le
// prénom d'un participant du groupe, lu avec l'acteur administrateur du groupe
// (borné au groupe demandé). letterpages ne l'appelle que pour la personne
// connectée ou pour une cible déjà découverte.
type letterNames struct{ svc *participants.Service }

// FirstName implémente letterpages.Names.
func (n letterNames) FirstName(ctx context.Context, groupID, participantID int64) (string, error) {
	p, err := n.svc.Get(ctx, participants.AdminActor(groupID), groupID, participantID)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", errs.NotFound("Cette personne n'existe pas dans le groupe.")
	}
	return strings.TrimSpace(p.FirstName), nil
}

var _ letterpages.Names = letterNames{}

// registerLetterPages monte les pages de la lettre (écriture par la personne
// connectée, lecture par le tireur) sur Slugs, derrière la chaîne
// SecurityHeaders, CSRF, Sessions. letterpages analyse un jeu de gabarits par
// page. La page de retour est la boîte aux lettres (/{slug}/boite) et
// l'espace de la personne /{slug}/participant. Elle suit registerLetter,
// registerLetterRead et registerTargetPage.
func registerLetterPages(a *App) error {
	p, err := letterpages.New(letterpages.Options{
		Groups: a.Groups, Letters: a.Letter, Reader: a.LetterRead, Targets: a.Draw,
		Names: letterNames{svc: a.Participants}, Translator: a.Cat, Clock: a.Clock, Location: a.Cfg.Location,
		MailboxURL: func(slug string) string { return slugPath(mailboxPath, slug) },
		HomeURL:    func(slug string) string { return "/" + slug + "/participant" },
	})
	if err != nil {
		return fmt.Errorf("pages de la lettre : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// registerMailboxPage monte la boîte aux lettres animée, GET /{slug}/boite
// (lecture seule). Les gabarits du paquet (mailbox/page, mailbox/error) sont
// deux documents complets : aucun ne définit layout/content.
func registerMailboxPage(a *App) error {
	set, err := newTemplateSet(a.Cat, "mailbox")
	if err != nil {
		return err
	}
	p, err := mailboxpage.New(mailboxpage.Deps{
		Groups: a.Groups, Participants: a.Participants, Draw: a.Draw, Letters: a.Letter, Stamps: a.LetterRead,
		Render: set.render, Clock: a.Clock, DefaultLang: a.Cfg.DefaultLang,
	})
	if err != nil {
		return fmt.Errorf("boîte aux lettres : %w", err)
	}
	a.Slugs.Handle("GET "+mailboxPath, p.Handler())
	return nil
}

// registerRegistrationAdmin monte l'écran « Inscriptions » de l'administration
// (interrupteur, lien, QR, plafond, inscriptions en attente) sur Slugs, derrière
// la chaîne SecurityHeaders, CSRF, Sessions. L'entrée de navigation est déclarée
// par registerAdminPages. Le gabarit layout/admin reçoit le bloc
// layout/content de web/templates/registration/admin : un jeu par page.
func registerRegistrationAdmin(a *App) error {
	set, err := newTemplateSet(a.Cat, "registration/admin")
	if err != nil {
		return err
	}
	p, err := regadmin.New(regadmin.Deps{
		Groups: a.Groups, Invite: a.Invite, Verify: a.Verify, Counter: invite.SQLDirectory{DB: a.DB},
		Draw: a.DrawState, Render: set.render, Nav: a.AdminNav, Clock: a.Clock, Location: a.Cfg.Location,
		PublicURL: a.publicBase(), DefaultLang: a.Cfg.DefaultLang,
	})
	if err != nil {
		return fmt.Errorf("inscriptions (administration) : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// adminDraws adapte le service du tirage au port drawpages.Draws : lancer et
// refaire le tirage passent par Drawing, qui met la révélation à jour (ouverture
// si elle est due, tâche planifiée à la date, avis « tirage prêt », reprise en
// cas d'échec). Les autres méthodes sont celles de draw.Service.
type adminDraws struct {
	*draw.Service
	drawing *Drawing
}

// Draw implémente drawpages.Draws.
func (d adminDraws) Draw(ctx context.Context, a participants.Actor, groupID int64, req draw.Request) (*draw.Outcome, error) {
	return d.drawing.Draw(ctx, a, groupID, req)
}

// Redraw implémente drawpages.Draws.
func (d adminDraws) Redraw(ctx context.Context, a participants.Actor, groupID int64, req draw.Request) (*draw.Outcome, error) {
	return d.drawing.Redraw(ctx, a, groupID, req)
}

var _ drawpages.Draws = adminDraws{}

// registerDrawPages monte l'onglet « Tirage » de l'administration (état, date de
// révélation, lancement confirmé, ouverture immédiate, historique) sur Slugs.
// Le tirage passe par adminDraws, donc par Drawing : drawpages.Options.OnDrawChanged
// reste vide, la mise à jour de la révélation est déjà faite (et retentée)
// par Drawing, et l'appeler une seconde fois ne ferait que doubler le travail. AdjustPath garde son défaut
// (/{slug}/admin/ajuster, page F10c à venir). NavFor résout {slug} dans la barre de navigation.
func registerDrawPages(a *App) error {
	p, err := drawpages.NewPages(drawpages.Options{
		Groups: a.Groups, Draws: adminDraws{Service: a.Draw, drawing: a.Drawing}, Reveal: a.Reveal,
		Translator: a.Cat, Audit: a.Audit, Location: a.Cfg.Location, NavFor: a.adminNav,
	})
	if err != nil {
		return fmt.Errorf("pages du tirage : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// registerPeoplePages monte la page des participants et la fiche d'une
// personne sur Slugs (/{slug}/admin/participants...). Les écritures sont
// consignées au journal ; le lien personnel est PUBLIC_URL/<slug>/acces/<jeton>
// comme à l'inscription ; les courriels passent par la file d'envoi (refus net
// sans transport).
func registerPeoplePages(a *App) error {
	base := a.publicBase()
	p, err := people.New(people.Options{
		DB: a.DB, Clock: a.Clock, Groups: a.Groups, People: a.Participants, Credentials: a.Tokens,
		Mail: mailEnqueuer{q: a.mail.q}, Unsub: a.Unsub, Rules: a.Rules, Changes: a.RuleChange,
		Audit: a.Audit, State: a.DrawState, Catalog: a.Cat,
		LinkURL:  func(slug, tok string) string { return base + "/" + slug + "/acces/" + tok },
		Location: a.Cfg.Location, Nav: a.adminNav,
	})
	if err != nil {
		return fmt.Errorf("pages des participants : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}
