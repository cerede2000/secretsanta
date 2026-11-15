// Package drawengine décide qui offre à qui à partir d'un jeu de contraintes.
//
// C'est un algorithme pur : aucun accès à la base ni au réseau. La
// persistance, la transaction qui l'entoure et la visibilité des cibles
// appartiennent à l'appelant.
//
// Modèle :
//   - chaque participant reçoit exactement un cadeau ;
//   - les personnes de rôle reception_seule n'en offrent aucun ;
//   - avec N participants dont R en réception seule, les G = N − R tireurs
//     offrent chacun au moins un cadeau et se partagent R cadeaux
//     supplémentaires : d'abord les offre_plus (répartition équitable,
//     floor ou ceil), puis les normal si la capacité des offre_plus ne
//     suffit pas, jamais les un_seul ; aucun tireur ne dépasse le plafond ;
//   - sans réception seule, l'affectation est une permutation.
//
// Le tirage suit la loi uniforme sur les affectations valides (tirage par
// rejet avec aléa cryptographique) ; au-delà d'un plafond d'essais, une
// recherche exhaustive à ordre aléatoire prend le relais. La faisabilité est
// contrôlée avant tout tirage par un flot maximum avec capacités et bornes
// inférieures (algorithme de Dinic, qui se réduit à Hopcroft-Karp quand toutes
// les capacités valent 1).
package drawengine

import (
	"sort"
)

// Role est le rôle d'une personne dans le tirage.
type Role string

const (
	// RoleNormal offre un cadeau, plus si les offre_plus ne suffisent pas.
	RoleNormal Role = "normal"
	// RoleReceptionOnly reçoit un cadeau et n'en offre aucun.
	RoleReceptionOnly Role = "reception_seule"
	// RoleGivesMore porte en priorité les cadeaux supplémentaires.
	RoleGivesMore Role = "offre_plus"
	// RoleOneGift offre exactement un cadeau, jamais plus.
	RoleOneGift Role = "un_seul"
)

// DefaultCap est le plafond de cadeaux offerts par personne quand l'entrée
// n'en fixe pas.
const DefaultCap = 2

// MaxUnconfirmedCap est le plafond le plus haut accepté sans confirmation
// explicite.
const MaxUnconfirmedCap = 2

// DefaultMaxAttempts est le nombre d'essais aléatoires avant la recherche
// exhaustive.
const DefaultMaxAttempts = 10000

// DefaultSearchBudget borne le nombre de contrôles de flot de la recherche
// exhaustive (seule l'option anti-échange peut la faire revenir en arrière).
const DefaultSearchBudget = 20000

// Participant est une personne du tirage. Household vide signifie « sans
// foyer ». Role vide vaut RoleNormal.
type Participant struct {
	ID        int64
	Household string
	Role      Role
}

// Pair est un lien orienté : Giver offre à Receiver.
type Pair struct {
	Giver    int64
	Receiver int64
}

// Input est le jeu de contraintes d'un tirage.
//
// Toutes les listes de paires sont orientées (Giver ne doit pas offrir à
// Receiver) ; une exclusion réciproque se donne par deux paires. Les paires
// d'exclusion, d'interdiction et d'historique qui citent une personne absente
// du tirage sont sans effet et ignorées ; un forçage qui cite une personne
// absente est refusé.
type Input struct {
	Participants []Participant
	// Exclusions sont les exclusions décidées par l'organisateur.
	Exclusions []Pair
	// Forced sont les forçages : Giver doit offrir à Receiver. Un forçage
	// consomme une capacité de son tireur.
	Forced []Pair
	// Forbidden sont des paires toujours interdites (par exemple l'historique
	// de plusieurs années).
	Forbidden []Pair
	// PreviousYear sont les paires de l'année précédente, exclues seulement
	// si ExcludePreviousYear est vrai.
	PreviousYear        []Pair
	ExcludePreviousYear bool
	// AntiSwap interdit qu'une personne A offre à B quand B offre à A.
	AntiSwap bool
	// Cap est le plafond de cadeaux offerts par personne ; 0 vaut DefaultCap.
	Cap int
	// CapConfirmed est l'indicateur de confirmation explicite, exigé pour un
	// plafond supérieur à MaxUnconfirmedCap.
	CapConfirmed bool
}

// Options règle le moteur.
type Options struct {
	// MaxAttempts est le nombre d'essais aléatoires avant la recherche
	// exhaustive ; 0 ou moins passe directement à la recherche.
	MaxAttempts int
	// SearchBudget borne la recherche exhaustive ; 0 ou moins vaut
	// DefaultSearchBudget.
	SearchBudget int
}

// DefaultOptions renvoie les réglages par défaut.
func DefaultOptions() Options {
	return Options{MaxAttempts: DefaultMaxAttempts, SearchBudget: DefaultSearchBudget}
}

// Result est une affectation : une paire par participant destinataire,
// triée par tireur puis par destinataire.
type Result struct {
	Pairs []Pair
	// Fallback indique que la recherche exhaustive a produit le résultat.
	Fallback bool
}

// Draw tire une affectation avec les réglages par défaut.
func Draw(in Input) (*Result, error) {
	return DrawWithOptions(in, DefaultOptions())
}

// DrawWithOptions tire une affectation valide uniformément au hasard
// (crypto/rand) ; au-delà de opts.MaxAttempts essais, une recherche
// exhaustive à ordre aléatoire prend le relais. Une instance sans solution
// renvoie une *Infeasible dont le message, en français, ne nomme personne.
func DrawWithOptions(in Input, opts Options) (*Result, error) {
	budget := opts.SearchBudget
	if budget <= 0 {
		budget = DefaultSearchBudget
	}
	p, inf := newProblem(in)
	if inf != nil {
		return nil, inf
	}
	if inf := p.precheck(); inf != nil {
		return nil, inf
	}
	full := p.constraints(0)
	if !p.flowFeasible(full, p.initialAssign(full)) {
		return nil, p.diagnose(budget)
	}
	for i := 0; i < opts.MaxAttempts; i++ {
		assign, err := p.sample()
		if err != nil {
			return nil, err
		}
		if p.valid(full, assign) {
			return p.result(assign, false), nil
		}
	}
	assign, status, err := p.search(full, budget)
	if err != nil {
		return nil, err
	}
	switch status {
	case searchFound:
		return p.result(assign, true), nil
	case searchExhausted:
		return nil, p.diagnose(budget)
	default:
		return nil, newInfeasible(CauseSearchLimit)
	}
}

// Check contrôle qu'au moins une affectation valide existe, sans en tirer.
func Check(in Input) error {
	p, inf := newProblem(in)
	if inf != nil {
		return inf
	}
	if inf := p.precheck(); inf != nil {
		return inf
	}
	status, err := p.solvable(p.constraints(0), DefaultSearchBudget)
	if err != nil {
		return err
	}
	switch status {
	case searchFound:
		return nil
	case searchExhausted:
		return p.diagnose(DefaultSearchBudget)
	default:
		return newInfeasible(CauseSearchLimit)
	}
}

func (p *problem) result(assign []int, fallback bool) *Result {
	pairs := make([]Pair, 0, p.n)
	for r, g := range assign {
		pairs = append(pairs, Pair{Giver: p.ids[g], Receiver: p.ids[r]})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Giver != pairs[j].Giver {
			return pairs[i].Giver < pairs[j].Giver
		}
		return pairs[i].Receiver < pairs[j].Receiver
	})
	return &Result{Pairs: pairs, Fallback: fallback}
}
