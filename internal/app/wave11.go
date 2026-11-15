package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"secretsanta/internal/admin/menu"
	"secretsanta/internal/givernotify"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildiagpages"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/reminders"
	"secretsanta/internal/reveal"
	"secretsanta/internal/suggestionpages"
)

// disabledMailer remplace la file d'envoi pour la page « Délivrabilité » quand
// aucune adresse d'expédition n'est configurée : le courriel de test est
// refusé net (ErrMailDisabled) et le diagnostic reste lisible.
type disabledMailer struct{}

// SendTest implémente maildiagpages.TestMailer.
func (disabledMailer) SendTest(context.Context, queue.Item) (queue.Status, error) {
	return queue.Status{}, ErrMailDisabled
}

// noBuilder tient la place du constructeur de messages sans adresse
// d'expédition : la page n'affiche alors aucun en-tête brut.
type noBuilder struct{}

// Build implémente queue.Builder.
func (noBuilder) Build(mailmsg.Message) (*mailmsg.Built, error) { return nil, ErrMailDisabled }

var (
	_ maildiagpages.TestMailer = disabledMailer{}
	_ queue.Builder            = noBuilder{}
)

// maildiagMenuLink est l'entrée de la feuille « Plus » vers le diagnostic de
// délivrabilité (les réglages du groupe, F10c, ne sont pas encore montés : la
// feuille Plus est la seule porte d'entrée de la page).
func maildiagMenuLink(slug string) menu.Link {
	return menu.Link{
		Href:     slugPath(maildiagpages.PagePath, slug),
		TitleKey: "admin.menu.maildiag", HintKey: "admin.menu.maildiag.hint", Icon: "mail",
	}
}

// registerMailDiagPages monte la page « Délivrabilité » de l'administration
// (diagnostic, enregistrements DNS, courriel de test, liste de suppression) sur
// Slugs. Le constructeur de messages et le signeur sont ceux de la file : la
// page reconstitue les en-têtes bruts avec eux (Message-ID et Date du message
// réellement envoyé peuvent différer). Sans adresse d'expédition, le courriel
// de test est refusé et aucun en-tête n'est affiché. Elle suit
// registerAdminPages (registre de navigation) et registerMailDiag.
func registerMailDiagPages(a *App) error {
	set, err := newTemplateSet(a.Cat, "maildiag")
	if err != nil {
		return err
	}
	deps := maildiagpages.Deps{
		Groups: a.Groups, Diagnostic: a.MailDiag, Suppressions: a.Suppress,
		Mailer: disabledMailer{}, Builder: noBuilder{},
		Catalog: a.Cat, Render: set.render, Nav: a.AdminNav, Clock: a.Clock,
		Location: a.Cfg.Location, DefaultLang: a.Cfg.DefaultLang,
	}
	if a.Mail != nil {
		deps.Builder = a.Mail
		deps.Signer = a.DKIM
	}
	if a.MailQueue != nil {
		deps.Mailer = a.MailQueue
	}
	p, err := maildiagpages.New(deps)
	if err != nil {
		return fmt.Errorf("page de délivrabilité : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// plusMenuRender ajoute à la feuille « Plus » les entrées propres à la vague 11
// (diagnostic de délivrabilité), puis passe la main à la consultation d'une paire.
func (a *App) plusMenuRender(next func(http.ResponseWriter, *http.Request, int, string, any)) func(http.ResponseWriter, *http.Request, int, string, any) {
	return a.consultMenuRender(func(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
		if pg, ok := data.(menu.Page); ok && pg.Slug != "" {
			pg.Links = append(pg.Links, maildiagMenuLink(pg.Slug))
			data = pg
		}
		next(w, r, status, name, data)
	})
}

// registerSuggestionPages monte la page des idées du tireur (F8c.2) :
// GET /{slug}/suggestions/{pair} et POST /{slug}/suggestions/{pair}/actualiser
// (RequireUnsafeMethod est dans la page). Les images viennent de /images/,
// monté par registerWeb. Sans service de suggestions, les routes ne sont pas
// montées. Elle suit registerSuggestionService et registerTargetPage dont le
// lien « idées » (targetpage.TargetIdeasPath) a le même chemin par défaut.
func registerSuggestionPages(a *App) error {
	if a.Suggestions == nil {
		return nil
	}
	set, err := newTemplateSet(a.Cat, "suggestions")
	if err != nil {
		return err
	}
	p, err := suggestionpages.New(suggestionpages.Deps{
		Groups: a.Groups, Participants: a.Participants, Draw: a.Draw, Suggest: a.Suggestions,
		Render: set.render, DefaultLang: a.Cfg.DefaultLang,
	})
	if err != nil {
		return fmt.Errorf("page des suggestions : %w", err)
	}
	a.Slugs.Handle("GET /{slug}/suggestions/{pair}", p.Show())
	a.Slugs.Handle("POST /{slug}/suggestions/{pair}/actualiser", p.Refresh())
	return nil
}

// registerGiverNotify construit la notification « nouvelle idée » et la relance
// du tireur (F7b) : abonnée au bus de letter.Service (mêmes événements) et
// passage régulier avec celui des relances. Une panne du gestionnaire remonte
// au bus, qui la journalise sans défaire l'enregistrement de la lettre
// (letter.Service ignore l'erreur de Publish après l'écriture). Sans file
// d'envoi (aucun transport), rien n'est abonné. La désinscription
// (mailunsub, MayNotify du moteur de relances) est consultée par le service
// lui-même (Unsub.ShouldSend). Elle suit registerReminders et registerLetter.
func registerGiverNotify(a *App) error {
	if a.mail.q == nil {
		return nil
	}
	if a.Bus == nil || a.Letter == nil {
		return errors.New("notification du tireur : bus et lettres requis")
	}
	svc := givernotify.New(givernotify.Options{
		DB: a.DB, Clock: a.Clock, Rules: a.ReminderRules,
		Directory: reminders.ServiceDirectory{DB: a.DB, Groups: a.Groups, People: a.Participants, Langs: a.GroupSettings},
		Queue:     a.mail.q, Unsub: a.Unsub, Suppress: a.Suppress, Catalog: a.Cat,
		Gate: reveal.NewGate(a.DB, a.Clock), Letters: a.Letter, PublicURL: a.publicBase(),
	})
	svc.Subscribe(a.Bus)
	a.Every("givernotify.tick", remindTickEvery, func(ctx context.Context) error {
		_, err := svc.Tick(ctx)
		return err
	})
	a.GiverNotify = svc
	return nil
}
