// Package reveal porte la révélation du tirage (F4b.2) : porte de
// révélation (date du groupe, ouverture immédiate sans date, « ouvrir
// maintenant »), modification de la date avec verrou et replanification de
// la tâche, ouverture automatique à la date avec l'avis « ton tirage est
// prêt », et faits exposés à l'administrateur.
//
// Règle de confidentialité : ce paquet ne lit ni ne renvoie jamais qui offre
// à qui. Il ne manipule que des identifiants de tirage et de cible et des
// nombres de cibles ; aucune erreur, aucun avis ni aucun fait n'en contient,
// et rien n'est journalisé. Les cibles elles-mêmes restent servies par
// draw.MyTargets, derrière la porte de ce paquet.
package reveal

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/scheduler"
)

// Module est le nom du module dans schema_migrations. Ses migrations
// s'appliquent après celles de groups et de draw.
const Module = "reveal"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par
// l'orchestrateur après celles de draw.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Manières dont une révélation a été ouverte.
const (
	// HowDate : la date de révélation est atteinte.
	HowDate = "date"
	// HowImmediate : le groupe n'a pas de date, la cible est immédiate.
	HowImmediate = "immediat"
	// HowManual : l'administrateur a choisi « ouvrir maintenant ».
	HowManual = "manuel"
)

// Messages français.
const (
	msgAuth         = "Authentification requise."
	msgForbidden    = "Tu n'as pas le droit d'effectuer cette action."
	msgNoGroup      = "Ce groupe n'existe pas."
	msgNotDone      = "Le tirage n'est pas encore fait."
	msgChanged      = "Le tirage a changé entre-temps. Recharge la page et recommence."
	msgConfirmOpen  = "L'ouverture doit être confirmée : les cibles deviendront visibles tout de suite pour chaque participant."
	msgDateLocked   = "La date de révélation ne peut plus être modifiée : la révélation est déjà ouverte ou une cible a déjà été découverte."
	msgDatePast     = "La date de révélation doit être dans le futur."
	msgDateInvalid  = "La date de révélation n'est pas valable."
	msgDateRequired = "Le tirage est fait : la date de révélation ne peut plus être retirée. Pour révéler les cibles tout de suite, utilisez « Ouvrir maintenant »."
)

// Codes d'erreur (pour les catalogues de textes de l'orchestrateur).
const (
	CodeDateLocked = "reveal.date_locked"
)

// DrawSource est la partie du service du tirage utile à la révélation
// (satisfaite par *draw.Service) : décomptes sans nom et faits sans paire.
type DrawSource interface {
	TargetCounts(ctx context.Context, groupID int64) (map[int64]int, error)
	AdminFacts(ctx context.Context, a participants.Actor, groupID int64) (*draw.Facts, error)
}

// Scheduler planifie la tâche d'ouverture (satisfait par
// *scheduler.Scheduler).
type Scheduler interface {
	At(at time.Time, t scheduler.Task) uint64
	Cancel(id uint64) bool
}

// ReadyNotice est la donnée du courriel « ton tirage est prêt » pour une
// personne : son nombre de cibles et rien d'autre. Aucun nom, aucune cible.
type ReadyNotice struct {
	GroupID       int64
	DrawID        int64
	ParticipantID int64
	// Targets vaut 0 (réception seule), 1 ou 2 (au plus le plafond).
	Targets int
	// ReceptionOnly vaut vrai quand la personne n'a aucune cible ; avec
	// Targets, il choisit la variante (templates.ReadyKind).
	ReceptionOnly bool
}

// ReadyMailer met en file le courriel « ton tirage est prêt » (branché par
// l'orchestrateur sur la file de F6). Une erreur laisse l'avis en attente :
// il sera repris au prochain Flush.
type ReadyMailer interface {
	EnqueueReady(ctx context.Context, n ReadyNotice) error
}

// Facts sont les faits de la révélation exposés à l'administrateur, sans
// aucune paire.
type Facts struct {
	// DrawDone indique que le tirage est fait.
	DrawDone bool
	// RevealAt est la date de révélation (UTC), nil sans date.
	RevealAt *time.Time
	// Open indique que la révélation est ouverte (révélation faite).
	Open bool
	// OpenedAt et OpenedHow décrivent l'ouverture enregistrée, s'il y en a
	// une (HowDate, HowImmediate ou HowManual).
	OpenedAt  *time.Time
	OpenedHow string
	// DateLocked indique que la date ne peut plus être modifiée.
	DateLocked bool
	// Targets et Discovered : nombre de cibles et nombre d'ouvertures.
	Targets    int
	Discovered int
}

const timeLayout = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	return t.UTC(), err
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// current est le tirage actuel d'un groupe.
type current struct {
	ok       bool
	id       int64
	revision int
}

func currentDraw(ctx context.Context, q queryer, groupID int64) (current, error) {
	var c current
	err := q.QueryRowContext(ctx, `SELECT id, revision FROM draws WHERE group_id = ? AND status = 'actuel'`, groupID).
		Scan(&c.id, &c.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return current{}, nil
	}
	if err != nil {
		return current{}, err
	}
	c.ok = true
	return c, nil
}

// errNoGroup signale un groupe inconnu.
var errNoGroup = errors.New("reveal: groupe inconnu")

// revealAt lit la date de révélation du groupe (nil sans date).
func revealAt(ctx context.Context, q queryer, groupID int64) (*time.Time, error) {
	var v sql.NullString
	err := q.QueryRowContext(ctx, `SELECT reveal_at FROM groups WHERE id = ?`, groupID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoGroup
	}
	if err != nil {
		return nil, err
	}
	if !v.Valid {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

// opening est l'ouverture enregistrée d'un tirage.
type opening struct {
	ok  bool
	how string
	at  time.Time
}

func openingOf(ctx context.Context, q queryer, drawID int64) (opening, error) {
	var o opening
	var at string
	err := q.QueryRowContext(ctx, `SELECT how, opened_at FROM reveal_openings WHERE draw_id = ?`, drawID).Scan(&o.how, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return opening{}, nil
	}
	if err != nil {
		return opening{}, err
	}
	if o.at, err = parseTime(at); err != nil {
		return opening{}, err
	}
	o.ok = true
	return o, nil
}

// state est l'état de la révélation du tirage actuel d'un groupe.
type state struct {
	draw     current
	revealAt *time.Time
	opening  opening
	// open : les cibles peuvent être renvoyées à leurs tireurs.
	open bool
	// due : l'ouverture automatique est due (sans date, ou date atteinte).
	due bool
	// discovered : au moins une cible du tirage actuel a été découverte.
	discovered bool
}

// locked indique que la date ne peut plus être modifiée : révélation
// ouverte (enregistrée, sans date ou date atteinte) ou cible découverte.
func (s state) locked() bool { return s.draw.ok && (s.open || s.discovered) }

func readState(ctx context.Context, q queryer, now time.Time, groupID int64) (state, error) {
	var st state
	var err error
	if st.revealAt, err = revealAt(ctx, q, groupID); err != nil {
		return state{}, err
	}
	st.due = st.revealAt == nil || !now.Before(*st.revealAt)
	if st.draw, err = currentDraw(ctx, q, groupID); err != nil {
		return state{}, err
	}
	if !st.draw.ok {
		return st, nil
	}
	if st.opening, err = openingOf(ctx, q, st.draw.id); err != nil {
		return state{}, err
	}
	st.open = st.opening.ok || st.due
	var n int
	if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM draw_pairs
		WHERE draw_id = ? AND discovered_at IS NOT NULL)`, st.draw.id).Scan(&n); err != nil {
		return state{}, err
	}
	st.discovered = n == 1
	return st, nil
}
