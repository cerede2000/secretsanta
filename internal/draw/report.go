package draw

import (
	"context"

	"secretsanta/internal/drawengine"
	"secretsanta/internal/participants"
)

// RoleCounts sont les personnes par rôle.
type RoleCounts struct {
	ReceptionSeule int
	OffrePlus      int
	Normal         int
	UnSeul         int
}

// CapacityLine est la ligne de capacité du contrôle.
type CapacityLine struct {
	// Cap est le plafond de cadeaux offerts par tireur.
	Cap int
	// ExtraGiftsNeeded est le nombre de cadeaux supplémentaires à offrir
	// (un par personne en réception seule).
	ExtraGiftsNeeded int
	// OffrePlusCapacity est la capacité des personnes prêtes à offrir plus.
	OffrePlusCapacity int
	// TotalCapacity ajoute la capacité des personnes au rôle normal.
	TotalCapacity int
}

// CheckReport est le rapport de contrôle : uniquement des comptes et des
// booléens, jamais un nom ni une paire.
type CheckReport struct {
	Beneficiaries   int
	GiversOneGift   int
	GiversTwoGifts  int
	GiversMoreGifts int // plus de 2 cadeaux (plafond confirmé)
	Roles           RoleCounts
	Capacity        CapacityLine
	// EachReceivesExactlyOne rappelle et vérifie que chaque participant
	// reçoit exactement un cadeau.
	EachReceivesExactlyOne bool
	// Drawn indique que les comptes de tireurs portent sur un tirage fait.
	Drawn bool
	// Green est le verdict vert (vrai) ou rouge (faux).
	Green bool
}

func baseReport(ro *participants.Roster, capN int) CheckReport {
	var r CheckReport
	r.Beneficiaries = len(ro.Entries)
	for _, e := range ro.Entries {
		switch e.Role {
		case participants.RoleReceptionSeule:
			r.Roles.ReceptionSeule++
		case participants.RoleOffrePlus:
			r.Roles.OffrePlus++
		case participants.RoleUnSeul:
			r.Roles.UnSeul++
		default:
			r.Roles.Normal++
		}
	}
	extra := max(capN-1, 0)
	r.Capacity = CapacityLine{
		Cap:               capN,
		ExtraGiftsNeeded:  r.Roles.ReceptionSeule,
		OffrePlusCapacity: r.Roles.OffrePlus * extra,
		TotalCapacity:     (r.Roles.OffrePlus + r.Roles.Normal) * extra,
	}
	return r
}

// buildReport contrôle une affectation contre la liste des participants.
func buildReport(ro *participants.Roster, capN int, pairs []Pair) CheckReport {
	r := baseReport(ro, capN)
	r.Drawn = true
	role := map[int64]participants.DrawRole{}
	for _, e := range ro.Entries {
		role[e.ID] = e.Role
	}
	received := map[int64]int{}
	given := map[int64]int{}
	ok := true
	for _, p := range pairs {
		if _, in := role[p.Receiver]; !in {
			ok = false
		}
		if _, in := role[p.Giver]; !in || p.Giver == p.Receiver {
			ok = false
		}
		received[p.Receiver]++
		given[p.Giver]++
	}
	exact := len(pairs) == len(ro.Entries)
	for id, rl := range role {
		if received[id] != 1 {
			exact = false
		}
		g := given[id]
		switch {
		case rl == participants.RoleReceptionSeule:
			if g != 0 {
				ok = false
			}
			continue
		case g == 0 || g > capN:
			ok = false
		case rl == participants.RoleUnSeul && g != 1:
			ok = false
		}
		switch {
		case g == 1:
			r.GiversOneGift++
		case g == 2:
			r.GiversTwoGifts++
		case g > 2:
			r.GiversMoreGifts++
		}
	}
	r.EachReceivesExactlyOne = exact
	r.Green = exact && ok
	return r
}

// Check contrôle, avant le tirage, la capacité et la faisabilité : il lit le
// plafond du groupe et les rôles, appelle le moteur sans tirer. Le rapport
// ne contient que des comptes ; une impossibilité est renvoyée en erreur
// (explication et propositions en français, sans nom).
func (s *Service) Check(ctx context.Context, a participants.Actor, groupID int64, capConfirmed bool) (*CheckReport, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	in, ro, capN, err := s.input(ctx, groupID, capConfirmed)
	if err != nil {
		return nil, err
	}
	r := baseReport(ro, capN)
	if err := drawengine.Check(in); err != nil {
		return &r, infeasibleError(err)
	}
	r.Green = true
	return &r, nil
}

// Report contrôle le tirage actuel (administrateur) : comptes seulement.
func (s *Service) Report(ctx context.Context, a participants.Actor, groupID int64) (*CheckReport, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	st, err := s.Status(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !st.Done {
		return nil, errNotDone()
	}
	ro, err := s.roster.Roster(ctx, groupID)
	if err != nil {
		return nil, err
	}
	var capN int
	if err := s.db.QueryRowContext(ctx, `SELECT gift_cap FROM draws WHERE id = ?`, st.DrawID).Scan(&capN); err != nil {
		return nil, internal(err)
	}
	pairs, err := s.drawPairs(ctx, st.DrawID)
	if err != nil {
		return nil, err
	}
	r := buildReport(ro, capN, pairs)
	return &r, nil
}
