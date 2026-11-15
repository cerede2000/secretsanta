package adjust

import (
	"testing"

	"pgregory.net/rapid"

	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
)

var allRoles = []drawengine.Role{
	drawengine.RoleNormal, drawengine.RoleGivesMore, drawengine.RoleOneGift, drawengine.RoleReceptionOnly,
}

// genAdjustment construit un tirage valide (moteur de tirage), des
// découvertes au hasard, puis un retrait ou un ajout.
func genAdjustment(t *rapid.T) (Problem, bool) {
	n := rapid.IntRange(3, 7).Draw(t, "n")
	capN := rapid.SampledFrom([]int{2, 3}).Draw(t, "cap")
	in := drawengine.Input{Cap: capN, CapConfirmed: true}
	for i := 0; i < n; i++ {
		role := rapid.SampledFrom(allRoles).Draw(t, "role")
		in.Participants = append(in.Participants, drawengine.Participant{ID: int64(i + 1), Role: role})
	}
	nEx := rapid.IntRange(0, 3).Draw(t, "exclusions")
	for i := 0; i < nEx; i++ {
		g := rapid.Int64Range(1, int64(n+1)).Draw(t, "exg")
		r := rapid.Int64Range(1, int64(n+1)).Draw(t, "exr")
		if g != r {
			in.Exclusions = append(in.Exclusions, drawengine.Pair{Giver: g, Receiver: r})
		}
	}
	res, err := drawengine.Draw(in)
	if err != nil {
		return Problem{}, false
	}
	p := Problem{Cap: capN}
	for _, e := range in.Exclusions {
		p.Exclusions = append(p.Exclusions, Pair{e.Giver, e.Receiver})
	}
	for _, q := range res.Pairs {
		p.Current = append(p.Current, CurrentPair{Giver: q.Giver, Receiver: q.Receiver,
			Discovered: rapid.Bool().Draw(t, "discovered")})
	}
	if rapid.Bool().Draw(t, "add") {
		for _, pe := range in.Participants {
			p.People = append(p.People, Person{ID: pe.ID, Role: pe.Role})
		}
		role := rapid.SampledFrom(allRoles).Draw(t, "newRole")
		p.People = append(p.People, Person{ID: int64(n + 1), Role: role})
	} else {
		gone := rapid.Int64Range(1, int64(n)).Draw(t, "removed")
		for _, pe := range in.Participants {
			if pe.ID != gone {
				p.People = append(p.People, Person{ID: pe.ID, Role: pe.Role})
			}
		}
	}
	return p, true
}

// F4c.AC15 : aucune réparation ni aucun ajustement ne fait offrir plus d'un
// cadeau à un un_seul ni plus que le plafond ; chaque personne reçoit
// exactement un cadeau, les exclusions sont respectées, et la solution est
// optimale (comparée à l'énumération exhaustive) : le moins de cibles
// découvertes changées, puis le moins de paires changées, puis la priorité
// aux offre_plus.
func TestPropertyAdjustCapacity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p, ok := genAdjustment(t)
		if !ok {
			t.Skip("tirage initial impossible")
		}
		best, feasible := bruteBest(p)
		sol, err := Solve(p)
		if !feasible {
			if err == nil {
				t.Fatalf("ajustement accepté sans solution : %+v", sol.Pairs)
			}
			return
		}
		if err != nil {
			t.Fatalf("ajustement refusé alors qu'une solution existe : %v", err)
		}
		roles := map[int64]participants.DrawRole{}
		for _, pe := range p.People {
			roles[pe.ID] = participants.DrawRole(pe.Role)
		}
		checkValid(t, sol.Pairs, roles, p.Cap)
		excl := map[Pair]bool{}
		for _, e := range p.Exclusions {
			excl[e] = true
		}
		for _, q := range sol.Pairs {
			if excl[q] {
				t.Fatalf("exclusion enfreinte : %v", q)
			}
		}
		if got := scoreOf(p, sol.Pairs); got != best {
			t.Fatalf("objectif %+v, optimum %+v", got, best)
		}
	})
}

// Les forçages sont respectés et le plafond relevé n'est jamais dépassé.
func TestPropertyAdjustForcedAndCap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p, ok := genAdjustment(t)
		if !ok {
			t.Skip("tirage initial impossible")
		}
		// Un forçage tiré parmi les paires actuelles encore valables.
		present := map[int64]bool{}
		for _, pe := range p.People {
			present[pe.ID] = true
		}
		var candidates []Pair
		for _, c := range p.Current {
			if present[c.Giver] && present[c.Receiver] {
				candidates = append(candidates, Pair{c.Giver, c.Receiver})
			}
		}
		if len(candidates) > 0 {
			p.Forced = []Pair{rapid.SampledFrom(candidates).Draw(t, "forced")}
		}
		best, feasible := bruteBest(p)
		sol, err := Solve(p)
		if feasible != (err == nil) {
			t.Fatalf("faisabilité %v, erreur %v", feasible, err)
		}
		if err != nil {
			return
		}
		roles := map[int64]participants.DrawRole{}
		for _, pe := range p.People {
			roles[pe.ID] = participants.DrawRole(pe.Role)
		}
		checkValid(t, sol.Pairs, roles, p.Cap)
		for _, f := range p.Forced {
			found := false
			for _, q := range sol.Pairs {
				found = found || q == f
			}
			if !found {
				t.Fatalf("forçage %v non respecté", f)
			}
		}
		if got := scoreOf(p, sol.Pairs); got != best {
			t.Fatalf("objectif %+v, optimum %+v", got, best)
		}
	})
}
