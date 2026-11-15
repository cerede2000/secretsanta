package adjust

import (
	"sort"

	"secretsanta/internal/drawengine"
)

// score est l'objectif lexicographique d'un tirage réparé.
type score struct {
	revealed int // cibles déjà découvertes abandonnées
	changed  int // paires nouvelles
	tier     int // cadeaux supplémentaires portés par des « normal »
}

func (a score) less(b score) bool {
	if a.revealed != b.revealed {
		return a.revealed < b.revealed
	}
	if a.changed != b.changed {
		return a.changed < b.changed
	}
	return a.tier < b.tier
}

// scoreOf calcule l'objectif d'un tirage réparé.
func scoreOf(p Problem, pairs []Pair) score {
	cur := map[Pair]bool{}
	for _, c := range p.Current {
		cur[Pair{c.Giver, c.Receiver}] = c.Discovered
	}
	present := map[int64]drawengine.Role{}
	for _, pe := range p.People {
		present[pe.ID] = pe.Role
	}
	next := map[Pair]bool{}
	give := map[int64]int{}
	var s score
	for _, q := range pairs {
		next[q] = true
		give[q.Giver]++
		if _, ok := cur[q]; !ok {
			s.changed++
		}
	}
	for _, c := range p.Current {
		_, okG := present[c.Giver]
		_, okR := present[c.Receiver]
		if okG && okR && c.Discovered && !next[Pair{c.Giver, c.Receiver}] {
			s.revealed++
		}
	}
	for id, role := range present {
		if (role == drawengine.RoleNormal || role == "") && give[id] > 1 {
			s.tier += give[id] - 1
		}
	}
	return s
}

// bruteBest énumère tous les tirages valides et renvoie le meilleur
// objectif ; ok est faux sans tirage valide.
func bruteBest(p Problem) (best score, ok bool) {
	capN := p.Cap
	if capN <= 0 {
		capN = drawengine.DefaultCap
	}
	people := append([]Person(nil), p.People...)
	sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
	present := map[int64]bool{}
	for _, pe := range people {
		present[pe.ID] = true
	}
	excl := map[Pair]bool{}
	for _, e := range p.Exclusions {
		excl[e] = true
	}
	forced := map[int64]int64{}
	for _, f := range p.Forced {
		if present[f.Giver] && present[f.Receiver] {
			forced[f.Receiver] = f.Giver
		}
	}
	var givers []Person
	limit := map[int64]int{}
	for _, pe := range people {
		l, _ := giftLimit(pe.Role, capN)
		limit[pe.ID] = l
		if l > 0 {
			givers = append(givers, pe)
		}
	}
	n := len(people)
	assign := make([]int64, n)
	give := map[int64]int{}
	var rec func(i int)
	rec = func(i int) {
		if i == n {
			for _, g := range givers {
				if give[g.ID] < 1 {
					return
				}
			}
			pairs := make([]Pair, n)
			for k := range people {
				pairs[k] = Pair{assign[k], people[k].ID}
			}
			s := scoreOf(p, pairs)
			if !ok || s.less(best) {
				best, ok = s, true
			}
			return
		}
		r := people[i].ID
		for _, g := range givers {
			if g.ID == r || excl[Pair{g.ID, r}] || give[g.ID] >= limit[g.ID] {
				continue
			}
			if f, isF := forced[r]; isF && f != g.ID {
				continue
			}
			assign[i] = g.ID
			give[g.ID]++
			rec(i + 1)
			give[g.ID]--
		}
	}
	rec(0)
	return best, ok
}
