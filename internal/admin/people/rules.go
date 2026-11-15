package people

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"secretsanta/internal/groups"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

const (
	kindExclusion = "exclusion"
	kindForced    = "forced"
	kindHousehold = "household"
	kindMember    = "member"
)

func idsOf(raw []string) ([]int64, bool) {
	var out []int64
	seen := map[int64]bool{}
	if len(raw) > 200 {
		return nil, false
	}
	for _, s := range raw {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, true
}

func oneID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// ruleOutcome est le résultat de l'application d'une règle.
type ruleOutcome struct {
	pending bool
	out     rulechange.Outcome
}

func (p *Pages) runRule(ctx context.Context, g *groups.Group, f url.Values, choice rulechange.Choice) (ruleOutcome, error) {
	c := p.o.Changes
	one := func(name string) (int64, error) {
		id, ok := oneID(f.Get(name))
		if !ok {
			return 0, errs.Validation("identifiant")
		}
		return id, nil
	}
	var res ruleOutcome
	note := func(o rulechange.Outcome, err error) error {
		if err != nil {
			return err
		}
		if o.Status == rulechange.StatusNeedsConfirmation {
			res.pending = true
			res.out.Status = o.Status
			res.out.Preview.AffectedCount += o.Preview.AffectedCount
			res.out.Preview.AlreadyDiscoveredCount += o.Preview.AlreadyDiscoveredCount
		}
		return nil
	}
	switch f.Get("kind") {
	case kindExclusion:
		a, err := one("a")
		if err != nil {
			return res, err
		}
		b, err := one("b")
		if err != nil {
			return res, err
		}
		if err := note(c.AddExclusion(ctx, g.ID, a, b, choice)); err != nil {
			return res, err
		}
		if f.Get("both") == "1" && !res.pending {
			if err := note(c.AddExclusion(ctx, g.ID, b, a, choice)); err != nil {
				return res, err
			}
		}
	case kindForced:
		a, err := one("a")
		if err != nil {
			return res, err
		}
		b, err := one("b")
		if err != nil {
			return res, err
		}
		return res, note(c.AddForced(ctx, g.ID, a, b, choice))
	case kindHousehold:
		ids, ok := idsOf(f["members"])
		if !ok {
			return res, errs.Validation("membres")
		}
		return res, note(c.CreateHousehold(ctx, g.ID, f.Get("name"), ids, choice))
	case kindMember:
		hid, err := one("household")
		if err != nil {
			return res, err
		}
		pid, err := one("a")
		if err != nil {
			return res, err
		}
		return res, note(c.AddHouseholdMember(ctx, g.ID, hid, pid, choice))
	default:
		return res, errs.Validation("type de règle")
	}
	return res, nil
}

// ApplyRule ajoute une exclusion, un forçage, un foyer ou un membre de foyer
// (POST .../regles). Après le tirage, un changement qui contredit une paire
// renvoie un aperçu en comptes, sans nom, avant toute écriture.
func (p *Pages) ApplyRule() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		if !parseForm(w, r) {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		choice := rulechange.ChoiceAsk
		switch r.PostForm.Get("choice") {
		case "repair":
			choice = rulechange.ChoiceRepair
		case "future":
			choice = rulechange.ChoiceFutureOnly
		}
		res, err := p.runRule(r.Context(), g, r.PostForm, choice)
		if err != nil {
			v, berr := p.buildList(r.Context(), g, url.Values{})
			if berr != nil {
				p.fail(w, r, berr)
				return
			}
			v.ErrorKey = errorKey(err, "admin.people.error.rule_conflict")
			v.ErrorField = "rule"
			p.render(w, r, statusOf(err), v)
			return
		}
		if res.pending {
			v, berr := p.buildList(r.Context(), g, url.Values{})
			if berr != nil {
				p.fail(w, r, berr)
				return
			}
			c := &confirmRule{Affected: res.out.Preview.AffectedCount, Discovered: res.out.Preview.AlreadyDiscoveredCount}
			for k, vals := range r.PostForm {
				if k == websec.CSRFFieldName || k == "choice" {
					continue
				}
				for _, val := range vals {
					c.Fields = append(c.Fields, field{Name: k, Value: val})
				}
			}
			v.RuleConfirm = c
			p.render(w, r, http.StatusOK, v)
			return
		}
		p.journal(r.Context(), g, ActionRuleChange, 0)
		http.Redirect(w, r, p.base(g)+"?ok=rule", http.StatusSeeOther)
	}))
}

// RemoveRule retire une exclusion, un forçage ou un membre de foyer
// (POST .../regles/retirer). Lever une contrainte ne contredit aucune paire.
func (p *Pages) RemoveRule() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		if !parseForm(w, r) {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		f := r.PostForm
		a, aok := oneID(f.Get("a"))
		b, bok := oneID(f.Get("b"))
		hid, hok := oneID(f.Get("household"))
		var err error
		switch f.Get("kind") {
		case kindExclusion:
			if !aok || !bok {
				err = errs.Validation("identifiant")
				break
			}
			err = p.o.Changes.RemoveExclusion(r.Context(), g.ID, a, b)
			if err == nil && f.Get("both") == "1" {
				err = p.o.Changes.RemoveExclusion(r.Context(), g.ID, b, a)
			}
		case kindForced:
			if !aok || !bok {
				err = errs.Validation("identifiant")
				break
			}
			err = p.o.Changes.RemoveForced(r.Context(), g.ID, a, b)
		case kindMember:
			if !aok || !hok {
				err = errs.Validation("identifiant")
				break
			}
			err = p.o.Changes.RemoveHouseholdMember(r.Context(), g.ID, hid, a)
		default:
			err = errs.Validation("type de règle")
		}
		if err != nil {
			v, berr := p.buildList(r.Context(), g, url.Values{})
			if berr != nil {
				p.fail(w, r, berr)
				return
			}
			v.ErrorKey, v.ErrorField = errorKey(err, "admin.people.error.rule_conflict"), "rule"
			p.render(w, r, statusOf(err), v)
			return
		}
		p.journal(r.Context(), g, ActionRuleChange, 0)
		http.Redirect(w, r, p.base(g)+"?ok=removed", http.StatusSeeOther)
	}))
}
