// Package mailboxpage porte la boîte aux lettres animée (F5c.2), fil
// conducteur de la lettre : « Ma lettre au Père Noël » et une lettre reçue
// par cible déjà découverte.
//
// Règles :
//   - La page est en lecture seule : aucun gestionnaire n'écrit, un autre
//     verbe que GET ou HEAD reçoit 405.
//   - Seules les cibles de la personne connectée et déjà découvertes
//     apparaissent (« Cadeau 1 », « Cadeau 2 ») ; aucune autre identité que la
//     cible n'est montrée, jamais celle d'un tireur.
//   - Une personne en réception seule ne voit que « Ma lettre ».
//   - L'état de la lettre (vide, brouillon, envoyée, lue) est déduit de la
//     lettre : vide = jamais écrite (version 0) ; brouillon = écrite sans
//     texte d'introduction ; envoyée = avec introduction, pas encore lue ;
//     lue = cachet posé sur la version actuelle.
//
// Le paquet ne connaît ni les routes ni l'analyse des gabarits :
// l'orchestrateur fournit un Renderer et monte Handler sur
// « GET /{slug}/boite ».
package mailboxpage

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/letter"
	"secretsanta/internal/letterread"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// Noms des gabarits (web/templates/mailbox/*.html) à analyser avec le layout.
const (
	TemplatePage  = "mailbox/page"
	TemplateError = "mailbox/error"
)

// États de la boîte aux lettres.
const (
	StateEmpty = "empty"
	StateDraft = "draft"
	StateSent  = "sent"
	StateRead  = "read"
)

// GroupSource est ce que la page demande au service des groupes.
type GroupSource interface {
	BySlug(ctx context.Context, slug string) (*groups.Group, error)
}

// ParticipantSource : *participants.Service.
type ParticipantSource interface {
	Get(ctx context.Context, a participants.Actor, groupID, id int64) (*participants.Participant, error)
}

// DrawSource : *draw.Service (cibles de la personne, derrière la porte de
// révélation).
type DrawSource interface {
	MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*draw.MyTargets, error)
}

// LetterSource : *letter.Service (lecture seule ; seule la version d'une
// lettre reçue est utilisée, jamais son contenu).
type LetterSource interface {
	Get(ctx context.Context, groupID, participantID int64) (*letter.Letter, error)
}

// StampSource : *letterread.Service (cachet de la lettre de la personne).
type StampSource interface {
	Stamp(ctx context.Context, a participants.Actor, groupID int64) (letterread.Stamp, error)
	// ReceivedStates donne, par paire, l'état de lecture de la lettre reçue
	// (letterread.ReceivedUnread, ReceivedNew, ReceivedSeen).
	ReceivedStates(ctx context.Context, a participants.Actor, groupID int64) (map[int64]string, error)
}

// Renderer écrit la page name avec le statut donné.
type Renderer func(w http.ResponseWriter, r *http.Request, status int, name string, data any)

// Deps regroupe les dépendances.
type Deps struct {
	Groups       GroupSource
	Participants ParticipantSource
	Draw         DrawSource
	Letters      LetterSource
	Stamps       StampSource
	Render       Renderer
	Clock        clock.Clock
	// DefaultLang sert si la langue n'a pas été négociée (« fr » si vide).
	DefaultLang string
	// Chemins des pages voisines ; valeurs par défaut indiquées.
	// OwnLetterPath : « /<slug>/lettre ».
	OwnLetterPath func(slug string) string
	// TargetLetterPath : « /<slug>/lettre-recue/<paire> ».
	TargetLetterPath func(slug string, pairID int64) string
	// HomePath : « /<slug>/participant ».
	HomePath func(slug string) string
}

// Received est l'entrée d'une lettre reçue.
type Received struct {
	N, Total int
	Written  bool
	URL      string
	// Read est l'état de lecture d'une lettre écrite : letterread.ReceivedUnread
	// (« À lire »), ReceivedNew (« Nouvelle idée ») ou ReceivedSeen (« Rien de
	// nouveau »). Vide quand la lettre n'est pas écrite.
	Read string
}

// Page est la donnée des gabarits du paquet.
type Page struct {
	Lang      string
	TitleKey  string
	ErrorKey  string
	Slug      string
	GroupName string
	Year      int
	BackURL   string
	OwnURL    string
	// State est l'état de la lettre de la personne (StateEmpty…StateRead).
	State    string
	Received []Received
	// TwoTargets : deux cibles, la note sur la deuxième lettre s'affiche.
	TwoTargets bool
	// ReceptionOnly : la personne n'offre rien, seule sa lettre apparaît.
	ReceptionOnly bool
}

// Pages construit les gestionnaires.
type Pages struct{ d Deps }

// New valide les dépendances et pose les valeurs par défaut.
func New(d Deps) (*Pages, error) {
	if d.Groups == nil || d.Participants == nil || d.Draw == nil || d.Letters == nil || d.Stamps == nil || d.Render == nil || d.Clock == nil {
		return nil, errors.New("mailboxpage : Groups, Participants, Draw, Letters, Stamps, Render et Clock sont obligatoires")
	}
	if d.DefaultLang == "" {
		d.DefaultLang = "fr"
	}
	if d.OwnLetterPath == nil {
		d.OwnLetterPath = func(slug string) string { return "/" + slug + "/lettre" }
	}
	if d.TargetLetterPath == nil {
		d.TargetLetterPath = func(slug string, id int64) string {
			return "/" + slug + "/lettre-recue/" + strconv.FormatInt(id, 10)
		}
	}
	if d.HomePath == nil {
		d.HomePath = func(slug string) string { return "/" + slug + "/participant" }
	}
	return &Pages{d: d}, nil
}

func (p *Pages) lang(r *http.Request) string {
	return negotiate.Lang(r.Context(), p.d.DefaultLang)
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, status int, key string) {
	p.d.Render(w, r, status, TemplateError, Page{Lang: p.lang(r), TitleKey: "mailbox.error.title", ErrorKey: key})
}

// Handler sert la boîte aux lettres (GET et HEAD seulement ; ne modifie
// rien). Sans session participant du groupe : renvoi vers la connexion.
func (p *Pages) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		g, err := p.d.Groups.BySlug(ctx, r.PathValue("slug"))
		if err != nil {
			if errs.Is(err, errs.KindNotFound) {
				p.fail(w, r, http.StatusNotFound, "core.errors.not_found")
			} else {
				p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
			}
			return
		}
		login := func() { http.Redirect(w, r, "/"+g.Slug+"/connexion", http.StatusSeeOther) }
		sess, found := websec.ParticipantSession(ctx, strconv.FormatInt(g.ID, 10))
		if !found {
			login()
			return
		}
		pid, err := strconv.ParseInt(sess.Subject, 10, 64)
		if err != nil || pid <= 0 {
			login()
			return
		}
		actor := participants.ParticipantActor(g.ID, pid)
		me, err := p.d.Participants.Get(ctx, actor, g.ID, pid)
		if err != nil {
			if errs.Is(err, errs.KindNotFound) || errs.Is(err, errs.KindUnauthorized) || errs.Is(err, errs.KindForbidden) {
				login()
				return
			}
			p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
			return
		}
		pg := Page{
			Lang: p.lang(r), TitleKey: "mailbox.title", Slug: g.Slug, GroupName: g.Name,
			Year:    p.d.Clock.Now().Year(),
			BackURL: p.d.HomePath(g.Slug), OwnURL: p.d.OwnLetterPath(g.Slug),
			ReceptionOnly: me.Role == participants.RoleReceptionSeule,
		}
		own, err := p.d.Letters.Get(ctx, g.ID, pid)
		if err != nil {
			p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
			return
		}
		pg.State = StateEmpty
		if own.Version > 0 {
			pg.State = StateSent
			if strings.TrimSpace(own.Intro) == "" {
				pg.State = StateDraft
			}
			st, err := p.d.Stamps.Stamp(ctx, actor, g.ID)
			if err != nil {
				p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
				return
			}
			if st.Read {
				pg.State = StateRead
			}
		}
		if !pg.ReceptionOnly {
			mine, err := p.d.Draw.MyTargets(ctx, actor, g.ID)
			if err != nil {
				p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
				return
			}
			if mine.Ready {
				states, err := p.d.Stamps.ReceivedStates(ctx, actor, g.ID)
				if err != nil {
					p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
					return
				}
				total := len(mine.Targets)
				pg.TwoTargets = total > 1
				for i, t := range mine.Targets {
					if !t.Discovered {
						continue
					}
					l, err := p.d.Letters.Get(ctx, g.ID, t.ReceiverID)
					if err != nil {
						p.fail(w, r, http.StatusInternalServerError, "core.errors.internal")
						return
					}
					pg.Received = append(pg.Received, Received{
						N: i + 1, Total: total, Written: l.Version > 0,
						URL: p.d.TargetLetterPath(g.Slug, t.PairID), Read: states[t.PairID],
					})
				}
			}
		}
		p.d.Render(w, r, http.StatusOK, TemplatePage, pg)
	})
}
