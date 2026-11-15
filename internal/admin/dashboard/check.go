package dashboard

import (
	"context"
	"net/mail"
	"strings"

	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/errs"
)

// Rule désigne une règle du contrôle du tirage.
type Rule string

// Règles contrôlées, dans l'ordre d'affichage.
const (
	RuleEachReceivesOne  Rule = "each_receives_one"   // chacun reçoit exactement un cadeau
	RuleGiversAtLeastOne Rule = "givers_at_least_one" // chaque tireur en offre au moins un
	RuleWithinCap        Rule = "within_cap"          // au plus le plafond du groupe
	RuleOneGiftOnly      Rule = "one_gift_only"       // les un_seul n'en offrent qu'un
	RuleNoSelf           Rule = "no_self"             // personne ne se tire soi-même
	RuleExclusions       Rule = "exclusions"          // exclusions respectées
	RuleEmailsValid      Rule = "emails_valid"        // courriels valides
	RuleLinksSent        Rule = "links_sent"          // liens envoyés
)

// RuleResult est le verdict d'une règle. Violations est un compte de
// personnes ou d'affectations, jamais un nom.
type RuleResult struct {
	Rule       Rule
	OK         bool
	Violations int
}

// RoleCounts sont les personnes par rôle.
type RoleCounts struct {
	Normal, OffrePlus, UnSeul, ReceptionSeule int
}

// CheckResult est le rapport du contrôle : des booléens et des comptes, aucun
// nom, aucune paire.
type CheckResult struct {
	// Green est vrai quand toutes les règles sont respectées.
	Green bool
	Rules []RuleResult
	// Beneficiaries : personnes qui reçoivent un cadeau.
	Beneficiaries int
	// Tireurs par nombre de cadeaux offerts.
	GiversOneGift   int
	GiversTwoGifts  int
	GiversMoreGifts int
	Roles           RoleCounts
	// Cap est le plafond de cadeaux par tireur du tirage.
	Cap int
}

// checkInput est tout ce que l'évaluation lit ; elle est pure.
type checkInput struct {
	people     []checkPerson
	pairs      []drawPair
	cap        int
	exclusions []rules.Pair
}

type checkPerson struct {
	id       int64
	role     participants.DrawRole
	email    string
	everSent bool
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && !strings.ContainsAny(s, " <>")
}

// evaluate applique les règles du contrôle.
func evaluate(in checkInput) CheckResult {
	var r CheckResult
	r.Cap = in.cap
	r.Beneficiaries = len(in.people)
	role := map[int64]participants.DrawRole{}
	for _, p := range in.people {
		role[p.id] = p.role
		switch p.role {
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
	received := map[int64]int{}
	given := map[int64]int{}
	excluded := map[rules.Pair]bool{}
	for _, e := range in.exclusions {
		excluded[e] = true
	}
	var self, excl int
	for _, p := range in.pairs {
		received[p.receiver]++
		given[p.giver]++
		if p.giver == p.receiver {
			self++
		}
		if excluded[rules.Pair{Giver: p.giver, Receiver: p.receiver}] {
			excl++
		}
	}
	var notOne, noGift, overCap, notOneOnly int
	for _, p := range in.people {
		if received[p.id] != 1 {
			notOne++
		}
		g := given[p.id]
		switch {
		case p.role == participants.RoleReceptionSeule:
			if g != 0 {
				overCap++ // plafond de 0 : une personne en réception seule n'offre rien
			}
			continue
		case g == 0:
			noGift++
		case g > in.cap:
			overCap++
		}
		if p.role == participants.RoleUnSeul && g > 1 {
			notOneOnly++
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
	for id := range received {
		if _, known := role[id]; !known {
			notOne++ // un destinataire qui n'est plus du groupe
		}
	}
	var badMail, notSent int
	for _, p := range in.people {
		if strings.TrimSpace(p.email) == "" {
			continue // sans courriel : le lien se remet en main propre
		}
		if !validEmail(p.email) {
			badMail++
		} else if !p.everSent {
			notSent++
		}
	}
	add := func(rule Rule, n int) {
		r.Rules = append(r.Rules, RuleResult{Rule: rule, OK: n == 0, Violations: n})
	}
	add(RuleEachReceivesOne, notOne)
	add(RuleGiversAtLeastOne, noGift)
	add(RuleWithinCap, overCap)
	add(RuleOneGiftOnly, notOneOnly)
	add(RuleNoSelf, self)
	add(RuleExclusions, excl)
	add(RuleEmailsValid, badMail)
	add(RuleLinksSent, notSent)
	r.Green = true
	for _, x := range r.Rules {
		if !x.OK {
			r.Green = false
		}
	}
	return r
}

// CheckDraw contrôle le tirage actuel : rapport vert ou rouge règle par règle,
// sans aucun nom. Sans tirage, la demande est refusée.
func (s *Service) CheckDraw(ctx context.Context, a participants.Actor, groupID int64) (*CheckResult, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	capN, pairs, ok, err := currentDraw(ctx, s.o.DB, groupID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errs.Conflict(msgNotDone)
	}
	list, err := s.o.Participants.List(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	cons, err := s.o.Constraints.BuildConstraints(ctx, groupID)
	if err != nil {
		return nil, err
	}
	act, err := loadActivity(ctx, s.o.DB, groupID)
	if err != nil {
		return nil, err
	}
	in := checkInput{pairs: pairs, cap: capN, exclusions: cons.Exclusions}
	for _, p := range list {
		in.people = append(in.people, checkPerson{id: p.ID, role: p.Role, email: strings.TrimSpace(p.Email), everSent: act.mail[p.ID].everSent})
	}
	res := evaluate(in)
	return &res, nil
}
