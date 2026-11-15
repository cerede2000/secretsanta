package adjust

import (
	"errors"
	"testing"

	"secretsanta/internal/drawengine"
	"secretsanta/internal/shared/errs"
)

// capacityRefused vérifie un refus de capacité sans nom, avec des
// propositions, et que rien n'a bougé.
func (e *env) capacityRefused(t *testing.T, gid int64, ch Change, cause drawengine.Cause) {
	t.Helper()
	before := e.snapshot(t, gid)
	p := e.preview(t, gid, ch)
	if p.Possible || p.Refusal == nil || !p.Refusal.Capacity {
		t.Fatalf("refus de capacité attendu : %+v", p)
	}
	if p.Refusal.Cause != cause {
		t.Fatalf("cause %q, attendu %q", p.Refusal.Cause, cause)
	}
	e.noNames(t, p.Refusal.Message)
	for _, s := range p.Refusal.Suggestions {
		e.noNames(t, s)
	}
	_, err := e.svc.Apply(ctx, admin(gid), gid, ch, Confirmation{Confirmed: true, Revision: p.Revision})
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("refus attendu, obtenu %v", err)
	}
	if code, _ := errs.CodeOf(err); code != CodeCapacity {
		t.Fatalf("code %q, attendu %q", code, CodeCapacity)
	}
	var ref *Refusal
	if !errors.As(err, &ref) || !ref.Capacity {
		t.Fatalf("le refus doit porter l'explication : %v", err)
	}
	e.noNames(t, err.Error())
	e.unchanged(t, gid, before)
}

// F4c.AC12 : trop de personnes ne peuvent pas offrir plus d'un cadeau —
// l'ajout d'une réception seule est refusé avec l'explication et les
// propositions, sans rien modifier.
func TestCapacityTooManyOneGift(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, unSeul, unSeul, unSeul)
	e.setPairs(t, gid, ring(ids...))
	w := e.add(t, gid, "Recoit", reception)
	e.capacityRefused(t, gid, addP(w), drawengine.CauseTooManyOneGift)

	p := e.preview(t, gid, addP(w))
	if len(p.Refusal.Suggestions) == 0 || p.Refusal.Missing != 1 {
		t.Fatalf("propositions et cadeaux manquants attendus : %+v", p.Refusal)
	}
}

// F4c.AC12 : plafond trop bas — chaque normal offre déjà deux cadeaux.
func TestCapacityCapTooLow(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, reception, reception)
	a, b, r1, r2 := ids[0], ids[1], ids[2], ids[3]
	e.setPairs(t, gid, []Pair{{a, b}, {a, r1}, {b, a}, {b, r2}})
	w := e.add(t, gid, "Recoit", reception)
	e.capacityRefused(t, gid, addP(w), drawengine.CauseCapTooLow)
}

// F4c.AC12 : retirer un tireur qui laisse trop peu de tireurs est refusé de
// la même façon.
func TestCapacityTooFewGiversAfterRemoval(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, reception)
	a, b, r := ids[0], ids[1], ids[2]
	e.setPairs(t, gid, []Pair{{a, b}, {a, r}, {b, a}})
	e.capacityRefused(t, gid, remove(b), drawengine.CauseTooFewGivers)
}
