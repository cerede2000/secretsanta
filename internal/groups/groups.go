// Package groups porte le modèle des groupes Secret Santa (F2) : slug
// immuable, budget facultatif, dates, options, création protégée par le mot
// de passe d'instance et vérification du secret d'administration.
package groups

import (
	"embed"
	"time"

	"secretsanta/internal/db"
)

// Module est le nom du module dans schema_migrations.
const Module = "groups"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées du module, à enregistrer par
// l'orchestrateur dans le registre commun.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Bornes des saisies.
const (
	SlugMinLen              = 3
	SlugMaxLen              = 40
	NameMaxLen              = 80
	GiftInstructionsMax     = 1000
	BudgetMinCents          = 100       // 1 €
	BudgetMaxCents          = 1_000_000 // 10 000 €
	DefaultMaxGiftsPerGiver = 2
	MinMaxGiftsPerGiver     = 2
	MaxMaxGiftsPerGiver     = 5
)

// DateLayout est le format de la date du jour J (date seule).
const DateLayout = "2006-01-02"

// Group est un groupe tel que lu en base. Il ne contient jamais le secret
// d'administration ni son condensé.
type Group struct {
	ID   int64
	Slug string
	Name string
	// BudgetCents vaut nil quand aucun budget n'est fixé.
	BudgetCents *int64
	// BudgetVersion vaut 0 à la création ; elle est incrémentée par le
	// service qui modifie le budget (F2e) pour signaler une mise à jour.
	BudgetVersion    int
	GiftInstructions string
	// EventDate est la date du jour J au format DateLayout, vide si absente.
	EventDate string
	// RevealAt est la date de révélation (UTC), nil si absente.
	RevealAt         *time.Time
	AdminCanViewPair bool
	MaxGiftsPerGiver int
	CreatedAt        time.Time
}

// HasBudget indique si un budget est fixé.
func (g Group) HasBudget() bool { return g.BudgetCents != nil }

// BudgetLabel renvoie le budget en français (« 25 € », « 25,50 € »), ou
// une chaîne vide quand aucun budget n'est fixé : rien ne doit alors être
// affiché.
func (g Group) BudgetLabel() string {
	if g.BudgetCents == nil {
		return ""
	}
	return FormatEuros(*g.BudgetCents)
}

// PublicView contient les seules données du groupe visibles sur la page
// publique /<slug> : le nom et le slug. Jamais de participant, de compte de
// participants ni de budget.
type PublicView struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}
