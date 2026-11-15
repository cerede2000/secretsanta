package drawengine

import "pgregory.net/rapid"

type genOpts struct {
	minN, maxN  int
	roles       bool // rôles variés plutôt que tous normal
	constraints bool // foyers, exclusions, historique, forçages
	capChoices  []int
}

// genInput produit une instance aléatoire, faisable ou non.
func genInput(t *rapid.T, o genOpts) Input {
	n := rapid.IntRange(o.minN, o.maxN).Draw(t, "n")
	roleGen := rapid.Just(RoleNormal)
	if o.roles {
		roleGen = rapid.SampledFrom([]Role{RoleNormal, RoleNormal, RoleGivesMore, RoleOneGift, RoleReceptionOnly, ""})
	}
	ps := make([]Participant, n)
	for i := range ps {
		ps[i] = Participant{ID: int64(1001 + i), Role: roleGen.Draw(t, "role")}
		if o.constraints {
			ps[i].Household = rapid.SampledFrom([]string{"", "", "", "maison-a", "maison-b"}).Draw(t, "foyer")
		}
	}
	in := Input{Participants: ps}
	if len(o.capChoices) > 0 {
		in.Cap = rapid.SampledFrom(o.capChoices).Draw(t, "plafond")
		in.CapConfirmed = rapid.Bool().Draw(t, "confirme")
	}
	in.AntiSwap = rapid.Bool().Draw(t, "anti_echange")
	if !o.constraints {
		return in
	}
	pair := func(label string) Pair {
		g := rapid.IntRange(0, n-1).Draw(t, label+"_g")
		r := rapid.IntRange(0, n-1).Draw(t, label+"_r")
		return Pair{Giver: ps[g].ID, Receiver: ps[r].ID}
	}
	for i, k := 0, rapid.IntRange(0, n).Draw(t, "nb_exclusions"); i < k; i++ {
		in.Exclusions = append(in.Exclusions, pair("exclusion"))
	}
	for i, k := 0, rapid.IntRange(0, n).Draw(t, "nb_interdites"); i < k; i++ {
		in.Forbidden = append(in.Forbidden, pair("interdite"))
	}
	for i, k := 0, rapid.IntRange(0, n).Draw(t, "nb_annee_prec"); i < k; i++ {
		in.PreviousYear = append(in.PreviousYear, pair("annee_prec"))
	}
	in.ExcludePreviousYear = rapid.Bool().Draw(t, "exclure_annee_prec")
	// Forçages vers une autre personne (le forçage vers soi-même a son test).
	for i, k := 0, rapid.IntRange(0, min(2, n-1)).Draw(t, "nb_forcages"); i < k; i++ {
		g := rapid.IntRange(0, n-1).Draw(t, "forcage_g")
		r := (g + rapid.IntRange(1, n-1).Draw(t, "forcage_d")) % n
		in.Forced = append(in.Forced, Pair{Giver: ps[g].ID, Receiver: ps[r].ID})
	}
	return in
}
