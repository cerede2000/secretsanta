package drawengine

import (
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/random"
)

// sample tire une affectation candidate qui respecte les quotas de niveau,
// sans tenir compte des autres contraintes.
//
// Loi : chaque vecteur de quotas admissible (répartition équitable floor/ceil
// dans chaque niveau, choix uniforme de ceux qui offrent un de plus) est
// tiré uniformément, puis les destinataires sont mélangés uniformément et
// distribués dans les places des tireurs. Une affectation de quotas q est
// alors obtenue avec probabilité ∏ q_g! / (|Q| · N!) ; le multiensemble des
// quotas étant le même pour tous les vecteurs admissibles, ce produit est
// constant : la loi est uniforme sur les affectations respectant les quotas,
// donc, après rejet, uniforme sur les affectations valides.
func (p *problem) sample() ([]int, error) {
	quota := make([]int, p.n)
	for t := 0; t < tierCount; t++ {
		s := p.tierSize[t]
		if s == 0 {
			continue
		}
		members := make([]int, 0, s)
		for i := 0; i < p.n; i++ {
			if p.tier[i] == t {
				members = append(members, i)
			}
		}
		if err := random.Shuffle(len(members), func(i, j int) {
			members[i], members[j] = members[j], members[i]
		}); err != nil {
			return nil, errs.Internal(err)
		}
		plus := p.tierTotal[t] - s*p.tierLo[t] // nombre de tireurs à un de plus
		for k, g := range members {
			quota[g] = p.tierLo[t]
			if k < plus {
				quota[g]++
			}
		}
	}
	slots := make([]int, 0, p.n)
	for g := 0; g < p.n; g++ {
		for k := 0; k < quota[g]; k++ {
			slots = append(slots, g)
		}
	}
	receivers := make([]int, p.n)
	for i := range receivers {
		receivers[i] = i
	}
	if err := random.Shuffle(len(receivers), func(i, j int) {
		receivers[i], receivers[j] = receivers[j], receivers[i]
	}); err != nil {
		return nil, errs.Internal(err)
	}
	assign := make([]int, p.n)
	for k, r := range receivers {
		assign[r] = slots[k]
	}
	return assign, nil
}

type searchStatus int

const (
	searchFound searchStatus = iota
	searchExhausted
	searchBudgetExceeded
)

// search est la recherche exhaustive : les destinataires sont pris dans un
// ordre aléatoire et, pour chacun, les tireurs possibles sont essayés dans un
// ordre aléatoire (jamais de décalage fixe). Chaque choix est validé par le
// contrôle de flot ; sans anti-échange ce contrôle est exact et la recherche
// ne revient jamais en arrière.
func (p *problem) search(c cons, budget int) ([]int, searchStatus, error) {
	assign := p.initialAssign(c)
	order := make([]int, 0, p.n)
	for r := 0; r < p.n; r++ {
		if assign[r] < 0 {
			order = append(order, r)
		}
	}
	if err := random.Shuffle(len(order), func(i, j int) {
		order[i], order[j] = order[j], order[i]
	}); err != nil {
		return nil, searchExhausted, errs.Internal(err)
	}
	s := &searcher{p: p, c: c, assign: assign, order: order, budget: budget}
	if !p.flowFeasible(c, assign) {
		return nil, searchExhausted, nil
	}
	found, err := s.run(0)
	if err != nil {
		return nil, searchExhausted, err
	}
	switch {
	case found:
		return assign, searchFound, nil
	case s.over:
		return nil, searchBudgetExceeded, nil
	default:
		return nil, searchExhausted, nil
	}
}

type searcher struct {
	p      *problem
	c      cons
	assign []int
	order  []int
	budget int
	over   bool
}

func (s *searcher) run(k int) (bool, error) {
	if k == len(s.order) {
		return s.p.valid(s.c, s.assign), nil
	}
	r := s.order[k]
	cands := make([]int, 0, s.p.n)
	for g := 0; g < s.p.n; g++ {
		if s.c.allowed[g][r] && !(s.c.antiSwap && s.assign[g] == r) {
			cands = append(cands, g)
		}
	}
	if err := random.Shuffle(len(cands), func(i, j int) {
		cands[i], cands[j] = cands[j], cands[i]
	}); err != nil {
		return false, errs.Internal(err)
	}
	for _, g := range cands {
		if s.budget <= 0 {
			s.over = true
			return false, nil
		}
		s.budget--
		s.assign[r] = g
		if s.p.flowFeasible(s.c, s.assign) {
			ok, err := s.run(k + 1)
			if ok || err != nil {
				return ok, err
			}
			if s.over {
				s.assign[r] = -1
				return false, nil
			}
		}
		s.assign[r] = -1
	}
	return false, nil
}

// solvable dit exactement si une affectation valide existe sous les
// contraintes c (dans la limite du budget de recherche).
func (p *problem) solvable(c cons, budget int) (searchStatus, error) {
	if !p.flowFeasible(c, p.initialAssign(c)) {
		return searchExhausted, nil
	}
	if !c.antiSwap {
		return searchFound, nil
	}
	_, status, err := p.search(c, budget)
	return status, err
}
