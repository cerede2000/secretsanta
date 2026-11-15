package adjust

import (
	"errors"
	"sort"

	"secretsanta/internal/drawengine"
)

// Pair est un lien orienté : Giver offre à Receiver. Usage interne au
// serveur ; jamais renvoyé à l'administrateur.
type Pair struct {
	Giver    int64
	Receiver int64
}

// CurrentPair est une paire du tirage actuel avec son drapeau de découverte.
type CurrentPair struct {
	Giver      int64
	Receiver   int64
	Discovered bool
}

// Person est un participant du tirage après le changement. Role vide vaut
// drawengine.RoleNormal.
type Person struct {
	ID   int64
	Role drawengine.Role
}

// Problem est une réparation à calculer : les personnes qui composent le
// tirage après le changement, les paires actuelles (elles peuvent citer une
// personne retirée) et les règles.
type Problem struct {
	People []Person
	// Current sont les paires du tirage actuel.
	Current []CurrentPair
	// Exclusions sont les couples interdits (foyers compris). Ceux qui citent
	// une personne absente sont ignorés.
	Exclusions []Pair
	// Forced sont les forçages. Ceux qui citent une personne absente sont
	// ignorés (la personne a quitté le tirage).
	Forced []Pair
	// Cap est le plafond de cadeaux offerts par personne ; 0 vaut
	// drawengine.DefaultCap.
	Cap int
}

// Solution est le tirage réparé : une paire par personne destinataire, triée
// par tireur puis par destinataire.
type Solution struct {
	Pairs []Pair
}

// errNoSolution signale qu'aucune affectation ne respecte les contraintes.
var errNoSolution = errors.New("adjust: aucune réparation")

// Poids lexicographiques de l'objectif (voir Solve).
type weights struct {
	tier     int64 // un cadeau supplémentaire porté par un « normal »
	change   int64 // une paire actuelle abandonnée
	revealed int64 // une cible déjà découverte abandonnée
	floor    int64 // un tireur sans aucun cadeau à offrir
}

func newWeights(n, capN int) weights {
	var w weights
	w.tier = 1
	w.change = int64(n)*int64(capN) + 1
	w.revealed = w.change * int64(n+1)
	w.floor = (w.revealed + w.change) * int64(n+1)
	return w
}

// giftLimit renvoie le nombre maximal de cadeaux offerts selon le rôle.
func giftLimit(role drawengine.Role, capN int) (int, bool) {
	switch role {
	case drawengine.RoleReceptionOnly:
		return 0, true
	case drawengine.RoleOneGift:
		return 1, true
	case drawengine.RoleNormal, drawengine.RoleGivesMore, "":
		return capN, true
	}
	return 0, false
}

// Solve calcule le tirage réparé qui respecte toutes les contraintes :
// chaque personne reçoit exactement un cadeau, personne ne s'offre son
// cadeau, une réception seule n'offre rien, un un_seul offre exactement un
// cadeau, tout autre tireur offre au moins un cadeau et au plus le plafond,
// les exclusions, foyers et forçages sont respectés.
//
// Parmi ces tirages, il choisit, dans cet ordre de priorité :
//  1. le moins de cibles déjà découvertes changées (dernier recours) ;
//  2. le moins de paires changées ;
//  3. le moins de cadeaux supplémentaires portés par des « normal » (les
//     cadeaux supplémentaires vont d'abord aux offre_plus).
//
// C'est un flot de coût minimal (plus courts chemins successifs avec
// potentiels) : la recherche est exacte. Sans solution, l'erreur est
// errNoSolution.
func Solve(p Problem) (*Solution, error) {
	capN := p.Cap
	if capN <= 0 {
		capN = drawengine.DefaultCap
	}
	people := append([]Person(nil), p.People...)
	sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
	n := len(people)
	if n == 0 {
		return nil, errNoSolution
	}
	index := make(map[int64]int, n)
	limit := make([]int, n)
	for i, pe := range people {
		if _, dup := index[pe.ID]; dup {
			return nil, errNoSolution
		}
		l, ok := giftLimit(pe.Role, capN)
		if !ok {
			return nil, errNoSolution
		}
		index[pe.ID] = i
		limit[i] = l
	}
	excluded := map[[2]int]bool{}
	for _, e := range p.Exclusions {
		g, okG := index[e.Giver]
		r, okR := index[e.Receiver]
		if okG && okR {
			excluded[[2]int{g, r}] = true
		}
	}
	forcedBy := make([]int, n)
	for i := range forcedBy {
		forcedBy[i] = -1
	}
	for _, f := range p.Forced {
		g, okG := index[f.Giver]
		r, okR := index[f.Receiver]
		if !okG || !okR {
			continue
		}
		if forcedBy[r] != -1 && forcedBy[r] != g {
			return nil, errNoSolution
		}
		forcedBy[r] = g
	}
	current := map[[2]int]bool{} // valeur : découverte
	for _, c := range p.Current {
		g, okG := index[c.Giver]
		r, okR := index[c.Receiver]
		if okG && okR {
			current[[2]int{g, r}] = c.Discovered
		}
	}

	w := newWeights(n, capN)
	var givers []int
	for i := range people {
		if limit[i] > 0 {
			givers = append(givers, i)
		}
	}
	// Nœuds : 0 source, 1..G tireurs, G+1..G+n destinataires, G+n+1 puits.
	G := len(givers)
	src, sink := 0, G+n+1
	f := newFlow(G + n + 2)
	floorEdge := make([]int, G)
	type arc struct{ edge, g, r int }
	var arcs []arc
	for k, g := range givers {
		floorEdge[k] = f.add(src, 1+k, 1, -w.floor)
		if extra := limit[g] - 1; extra > 0 {
			var c int64
			if people[g].Role != drawengine.RoleGivesMore {
				c = w.tier
			}
			f.add(src, 1+k, extra, c)
		}
		for r := 0; r < n; r++ {
			if r == g || excluded[[2]int{g, r}] {
				continue
			}
			if forcedBy[r] != -1 && forcedBy[r] != g {
				continue
			}
			var c int64
			if disc, ok := current[[2]int{g, r}]; ok {
				c = -w.change
				if disc {
					c -= w.revealed
				}
			}
			arcs = append(arcs, arc{f.add(1+k, 1+G+r, 1, c), g, r})
		}
	}
	for r := 0; r < n; r++ {
		f.add(1+G+r, sink, 1, 0)
	}
	if flow := f.run(src, sink); flow != n {
		return nil, errNoSolution
	}
	for _, e := range floorEdge {
		if f.cap[e] != 0 {
			return nil, errNoSolution
		}
	}
	sol := &Solution{}
	for _, a := range arcs {
		if f.cap[a.edge] == 0 {
			sol.Pairs = append(sol.Pairs, Pair{Giver: people[a.g].ID, Receiver: people[a.r].ID})
		}
	}
	sortPairs(sol.Pairs)
	return sol, nil
}

func sortPairs(p []Pair) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].Giver != p[j].Giver {
			return p[i].Giver < p[j].Giver
		}
		return p[i].Receiver < p[j].Receiver
	})
}

// flow est un réseau de flot de coût minimal (listes d'adjacence, arêtes
// résiduelles appariées : l'arête e et son inverse e^1).
type flow struct {
	n    int
	adj  [][]int
	to   []int
	cap  []int
	cost []int64
}

func newFlow(n int) *flow { return &flow{n: n, adj: make([][]int, n)} }

func (f *flow) add(u, v, c int, cost int64) int {
	e := len(f.to)
	f.to = append(f.to, v, u)
	f.cap = append(f.cap, c, 0)
	f.cost = append(f.cost, cost, -cost)
	f.adj[u] = append(f.adj[u], e)
	f.adj[v] = append(f.adj[v], e+1)
	return e
}

const inf = int64(1) << 62

// run envoie le flot maximal de coût minimal de s à t (plus courts chemins
// successifs, Dijkstra dense avec potentiels de Johnson) et renvoie sa
// valeur. Le réseau initial est sans circuit, donc sans cycle négatif.
func (f *flow) run(s, t int) int {
	pot := f.bellmanFord(s)
	dist := make([]int64, f.n)
	prev := make([]int, f.n)
	done := make([]bool, f.n)
	total := 0
	for {
		for i := range dist {
			dist[i], prev[i], done[i] = inf, -1, false
		}
		dist[s] = 0
		for {
			u := -1
			for v := 0; v < f.n; v++ {
				if !done[v] && dist[v] < inf && (u == -1 || dist[v] < dist[u]) {
					u = v
				}
			}
			if u == -1 {
				break
			}
			done[u] = true
			for _, e := range f.adj[u] {
				v := f.to[e]
				if f.cap[e] <= 0 || done[v] || pot[u] >= inf || pot[v] >= inf {
					continue
				}
				nd := dist[u] + f.cost[e] + pot[u] - pot[v]
				if nd < dist[v] {
					dist[v], prev[v] = nd, e
				}
			}
		}
		if dist[t] >= inf {
			return total
		}
		for v := 0; v < f.n; v++ {
			if dist[v] < inf {
				pot[v] += dist[v]
			}
		}
		push := int(^uint(0) >> 1)
		for v := t; v != s; v = f.to[prev[v]^1] {
			push = min(push, f.cap[prev[v]])
		}
		for v := t; v != s; v = f.to[prev[v]^1] {
			f.cap[prev[v]] -= push
			f.cap[prev[v]^1] += push
		}
		total += push
	}
}

// bellmanFord calcule les potentiels initiaux (distances depuis s ; inf pour
// un nœud inaccessible).
func (f *flow) bellmanFord(s int) []int64 {
	d := make([]int64, f.n)
	for i := range d {
		d[i] = inf
	}
	d[s] = 0
	for it := 0; it < f.n; it++ {
		changed := false
		for u := 0; u < f.n; u++ {
			if d[u] >= inf {
				continue
			}
			for _, e := range f.adj[u] {
				if f.cap[e] > 0 && d[u]+f.cost[e] < d[f.to[e]] {
					d[f.to[e]] = d[u] + f.cost[e]
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return d
}
