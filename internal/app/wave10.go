package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/adjust"
	"secretsanta/internal/admin/menu"
	"secretsanta/internal/adminconsult"
	"secretsanta/internal/adminops"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminders"
	"secretsanta/internal/repair"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/suggestions"
)

// remindTickEvery est la période du planificateur de relances : la fenêtre
// d'envoi et les délais se comptent en heures et en jours, la minute suffit.
const remindTickEvery = time.Minute

// registerReminders construit le moteur de relances (F7.2 à F7.4). Les faits
// qui arrêtent une relance (lien ouvert) sont toujours enregistrés, car
// registerAccessPages les pose ; le moteur, lui, n'existe que si la file
// d'envoi tourne (un transport est configuré) : sans elle, une relance mise en
// file ne partirait jamais. Le planificateur passe chaque minute (a.Every) ; il
// est sans état caché, donc sûr après un redémarrage. Elle suit
// registerMailUnsub, registerReminderRules et registerSuppression.
func registerReminders(a *App) error {
	a.ReminderFacts = reminders.SQLFacts{DB: a.DB, Clock: a.Clock}
	if a.mail.q == nil {
		return nil
	}
	e := reminders.New(reminders.Options{
		DB: a.DB, Clock: a.Clock, Rules: a.ReminderRules,
		Directory: reminders.ServiceDirectory{DB: a.DB, Groups: a.Groups, People: a.Participants, Langs: a.GroupSettings},
		Facts:     a.ReminderFacts, Queue: a.mail.q, Unsub: a.Unsub, Suppress: a.Suppress,
		Catalog: a.Cat, PublicURL: a.publicBase(),
	})
	a.Reminders = e
	a.Every("reminders.tick", remindTickEvery, func(ctx context.Context) error {
		_, err := e.Tick(ctx)
		return err
	})
	return nil
}

// linkOpenedCredentials note, à la première connexion par lien personnel,
// que la personne a ouvert son lien (fait qui arrête la relance « lien non
// ouvert »). Seul ResolveLink est concerné : la saisie du code court et le lien
// magique ne comptent pas. Une panne d'enregistrement est journalisée et ne
// refuse jamais la connexion.
type linkOpenedCredentials struct {
	accessCredentials
	facts reminders.SQLFacts
}

// accessCredentials est la part de token.Service utilisée par les pages de
// connexion (accesspages.Credentials).
type accessCredentials interface {
	ResolveCode(ctx context.Context, groupID, input string) (string, error)
	ResolveLink(ctx context.Context, groupID, tok string) (string, error)
}

// ResolveLink implémente accesspages.Credentials.
func (c linkOpenedCredentials) ResolveLink(ctx context.Context, groupID, tok string) (string, error) {
	pid, err := c.accessCredentials.ResolveLink(ctx, groupID, tok)
	if err != nil || pid == "" {
		return pid, err
	}
	g, errG := strconv.ParseInt(groupID, 10, 64)
	p, errP := strconv.ParseInt(pid, 10, 64)
	if errG != nil || errP != nil {
		return pid, nil
	}
	if err := c.facts.RecordLinkOpened(ctx, g, p); err != nil {
		slog.Error("ouverture du lien non enregistrée", "groupe", g, "erreur", err)
	}
	return pid, nil
}

// registerSuggestionService construit le service des suggestions de cadeaux
// (F8c.1) : tirage pour les cibles, lettre et participants pour les mots-clés,
// chaîne de sources et copie des images par la garde SSRF. Aucune route (la page
// F8c.2 viendra) ; sans l'une des dépendances, le service n'est pas construit.
// Elle suit registerImages, registerSuggestions, registerLetter et registerDraw.
func registerSuggestionService(a *App) error {
	if a.Draw == nil || a.Groups == nil || a.Letter == nil || a.Participants == nil ||
		a.Sources == nil || a.Images == nil || a.SSRF == nil {
		return nil
	}
	a.Suggestions = suggestions.NewService(a.DB, a.Clock, a.Draw, a.Groups, a.Letter, a.Participants,
		a.Sources, suggestions.GuardImages(a.Images, a.SSRF), suggestions.Options{Logger: slog.Default()})
	return nil
}

// repairing complète repair.Service : une réparation appliquée change les
// paires du tirage actuel, donc la révélation doit en être prévenue comme
// pour un ajustement (Adjusting) ou un nouveau tirage (Drawing).
type repairing struct {
	svc     *repair.Service
	drawing *Drawing
}

// Preview implémente ports.RuleChangeRepair.
func (r repairing) Preview(ctx context.Context, groupID int64, rule ports.ProposedRule) (ports.RepairPreview, error) {
	return r.svc.Preview(ctx, groupID, rule)
}

// Apply implémente ports.RuleChangeRepair : la règle et la réparation sont
// écrites dans une transaction, puis la révélation est mise à jour (une panne
// de celle-ci est journalisée et retentée, elle ne défait rien).
func (r repairing) Apply(ctx context.Context, groupID int64, rule ports.ProposedRule, write ports.RuleWriter) error {
	if err := r.svc.Apply(ctx, groupID, rule, write); err != nil {
		return err
	}
	r.drawing.changed(ctx, groupID)
	return nil
}

var _ ports.RuleChangeRepair = repairing{}

// registerRuleChange construit le service des modifications de règles et de
// rôles avec la réparation réelle du tirage (repair.Service) : même lecture du
// tirage, des rôles et des règles que l'ajustement, mêmes avis par la file
// d'envoi, archivage des lettres des anciennes cibles par letterread. Elle
// suit registerLetterRead et registerReveal.
//
// Écart connu : ports.ProposedRule ne porte pas le nouveau rôle, donc
// rulechange.SetRole ne peut pas appeler repair.Service.PreviewRole ;
// l'aperçu du port est celui, prudent, de repair.Service.Preview (le plus fort
// impact parmi les autres rôles). Apply calcule la réparation exacte.
func registerRuleChange(a *App) error {
	a.Repair = repair.NewService(a.DB, repair.Deps{
		Draw: a.Draw, Groups: a.Groups, Roster: a.Participants, Constraints: a.Rules,
		Directory: adjust.ParticipantDirectory{Participants: a.Participants},
		Mail:      a.mail, Archiver: letterArchiver{svc: a.LetterRead}, Compose: a.adjustComposer,
	})
	a.RuleChange = rulechange.NewService(a.DB, a.Clock, a.DrawState, repairing{svc: a.Repair, drawing: a.Drawing})
	return nil
}

// registerAdminConsult monte la consultation d'une paire (F10b.1) :
// GET et POST /{slug}/admin/consultation, GET /{slug}/consultations. Les
// routes répondent comme une route inexistante tant que l'option du groupe
// (groups.admin_can_view_pair) est désactivée. Elle suit registerAdminPages.
func registerAdminConsult(a *App) error {
	svc, err := adminconsult.NewService(a.DB, a.Clock, a.Groups, a.Participants)
	if err != nil {
		return fmt.Errorf("consultation de paire : %w", err)
	}
	p, err := adminconsult.NewPages(adminconsult.PageOptions{
		Service: svc, Groups: a.Groups, Translator: a.Cat, Location: a.Cfg.Location, NavFor: a.adminNav,
	})
	if err != nil {
		return fmt.Errorf("pages de consultation : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// registerAdminOps monte l'exploitation d'un groupe (F10b.2) : page des
// données, export des participants (GET), import, sauvegarde, réinitialisation
// de l'accès, duplication et suppression (POST). Les images des lettres d'un
// groupe purgé sont celles de registerLetter. Elle suit registerAdminPages.
func registerAdminOps(a *App) error {
	svc, err := adminops.NewService(a.DB, a.Clock, adminops.Options{ImageDir: filepath.Join(a.Cfg.DataDir, "letter-images")})
	if err != nil {
		return fmt.Errorf("exploitation : %w", err)
	}
	set, err := newTemplateSet(a.Cat, "adminops")
	if err != nil {
		return err
	}
	p, err := adminops.New(adminops.Deps{
		Groups: a.Groups, Ops: svc, Render: set.render, Nav: a.AdminNav,
		Location: a.Cfg.Location, DefaultLang: a.Cfg.DefaultLang,
	})
	if err != nil {
		return fmt.Errorf("pages d'exploitation : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// consultMenuRender ajoute à la feuille « Plus » l'entrée de la consultation
// d'une paire quand l'option du groupe est active ; sinon la feuille est
// rendue telle quelle.
func (a *App) consultMenuRender(next func(http.ResponseWriter, *http.Request, int, string, any)) func(http.ResponseWriter, *http.Request, int, string, any) {
	return func(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
		if pg, ok := data.(menu.Page); ok && pg.Slug != "" {
			if g, err := a.Groups.BySlug(r.Context(), pg.Slug); err == nil && g != nil && g.AdminCanViewPair {
				pg.Links = append(pg.Links, menu.Link{
					Href:     slugPath(adminconsult.RouteConsult, pg.Slug),
					TitleKey: "admin.menu.consult", HintKey: "admin.menu.consult.hint", Icon: "journal",
				})
				data = pg
			}
		}
		next(w, r, status, name, data)
	}
}

// registerBudgetNotice envoie le courriel « budget modifié » (modèle
// BudgetUpdated) quand l'administrateur a coché « Prévenir les participants »
// en changeant le budget (événement groupsettings.EventBudgetChanged). Le
// courriel est essentiel (aucune désinscription ne le coupe), ne nomme que le
// destinataire et le groupe, et part dans la langue du destinataire, sinon
// celle du groupe. Sans file d'envoi (aucun transport), rien n'est envoyé. Une
// panne ne fait jamais échouer l'enregistrement des réglages : elle est
// journalisée. Elle suit registerBus, registerGroupSettings et registerMailQueue.
func registerBudgetNotice(a *App) error {
	a.Bus.Subscribe(groupsettings.EventBudgetChanged, func(ctx context.Context, e ports.Event) error {
		if e.Payload["notify"] != "true" || a.mail.q == nil {
			return nil
		}
		if err := a.noticeBudget(ctx, e); err != nil {
			slog.Error("avis de changement de budget en échec", "groupe", e.GroupID, "erreur", err)
		}
		return nil
	})
	return nil
}

func (a *App) noticeBudget(ctx context.Context, e ports.Event) error {
	g, err := a.Groups.ByID(ctx, e.GroupID)
	if err != nil {
		return err
	}
	var budget *int64
	if v := e.Payload["new_budget"]; v != "" && v != groupsettings.BudgetNone {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("budget illisible : %w", err)
		}
		budget = &n
	}
	people, err := a.Participants.List(ctx, participants.AdminActor(g.ID), g.ID)
	if err != nil {
		return err
	}
	groupLang, _ := a.GroupSettings.GroupLanguage(ctx, g.Slug)
	for _, p := range people {
		if strings.TrimSpace(p.Email) == "" {
			continue
		}
		first := strings.TrimSpace(p.FirstName)
		if first == "" {
			first = localPart(p.Email)
		}
		m, err := templates.Render(a.Cat, templates.BudgetUpdated, templates.Data{
			To: p.Email, FirstName: first, GroupName: g.Name, ParticipantLang: p.Locale, GroupLang: groupLang,
			Link: a.publicBase() + "/" + g.Slug + "/participant", BudgetCents: budget,
		})
		if err != nil {
			return err
		}
		if _, err := a.mail.q.EnqueueMail(ctx, queue.Item{
			GroupID: g.ID, ParticipantID: p.ID, To: p.Email, ToName: p.FirstName,
			Type: string(templates.BudgetUpdated), Essential: m.Essential, Subject: m.Subject, Text: m.Text, HTML: m.HTML,
		}); err != nil {
			slog.Error("avis de changement de budget non mis en file", "groupe", g.ID, "participant", p.ID, "erreur", err)
		}
	}
	return nil
}
