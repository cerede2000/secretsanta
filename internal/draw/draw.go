// Package draw fait vivre le tirage (F4b.1) : tirage persistant en une seule
// transaction, historique par année, tirage complet refait avec archivage,
// verrou des inscriptions en attente, port DrawState et visibilité des
// cibles.
//
// Règle de confidentialité : qui offre à qui ne sort de ce paquet que vers la
// personne concernée, pour ses propres cibles. Aucune méthode ne renvoie les
// paires à l'administrateur, aucun message d'erreur ni événement n'en
// contient, et rien n'est journalisé.
package draw

import (
	"context"
	"embed"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
)

// Module est le nom du module dans schema_migrations. Ses migrations
// s'appliquent après celles de groups, participants et rules.
const Module = "draw"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par
// l'orchestrateur après groups, participants et rules.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Natures d'un tirage et d'un événement de l'historique.
const (
	KindDraw       = "tirage"
	KindRedraw     = "tirage_refait"
	KindArchive    = "archivage"
	KindAdjustment = "ajustement"
)

const (
	statusCurrent  = "actuel"
	statusArchived = "archive"
)

// GroupSource lit les réglages d'un groupe (satisfait par *groups.Service).
type GroupSource interface {
	ByID(ctx context.Context, id int64) (*groups.Group, error)
}

// RosterSource lit les participants et leurs rôles, sans nom (satisfait par
// *participants.Service).
type RosterSource interface {
	Roster(ctx context.Context, groupID int64) (*participants.Roster, error)
}

// ConstraintSource lit les exclusions et forçages (satisfait par
// *rules.Service).
type ConstraintSource interface {
	BuildConstraints(ctx context.Context, groupID int64) (rules.Constraints, error)
}

// RevealGate dit si les cibles d'un groupe peuvent être renvoyées à leurs
// tireurs. La révélation (F4b.2) fournit la sienne (date, « ouvrir
// maintenant ») ; par défaut, la cible est fermée tant que la date de
// révélation du groupe n'est pas atteinte, ouverte sans date.
type RevealGate interface {
	IsOpen(ctx context.Context, groupID int64) (bool, error)
}

// Pair est un lien orienté : Giver offre à Receiver. Usage interne au
// serveur (ajustement) ; jamais renvoyé à l'administrateur.
type Pair struct {
	Giver    int64
	Receiver int64
}

// Status est l'état du tirage actuel d'un groupe, sans paire.
type Status struct {
	Done     bool
	DrawID   int64
	Year     int
	Kind     string
	Revision int
	DrawnAt  time.Time
}

const timeLayout = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	return t.UTC(), err
}
