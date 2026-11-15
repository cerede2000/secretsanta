package adjust

import (
	"errors"
	"testing"

	"secretsanta/internal/shared/errs"
)

// snapshot est l'état observable d'un groupe pour vérifier que rien n'a
// bougé.
type snapshot struct {
	rows         []row
	revision     int
	events       int
	participants int
}

func (e *env) snapshot(t *testing.T, gid int64) snapshot {
	t.Helper()
	s := snapshot{rows: e.rows(t, gid)}
	if err := e.db.QueryRow(`SELECT revision FROM draws WHERE group_id = ? AND status = 'actuel'`, gid).Scan(&s.revision); err != nil {
		t.Fatal(err)
	}
	s.events = e.count(t, `SELECT COUNT(*) FROM draw_events WHERE group_id = ?`, gid)
	s.participants = e.count(t, `SELECT COUNT(*) FROM participants WHERE group_id = ?`, gid)
	return s
}

func (e *env) unchanged(t *testing.T, gid int64, before snapshot) {
	t.Helper()
	after := e.snapshot(t, gid)
	if len(after.rows) != len(before.rows) || after.revision != before.revision ||
		after.events != before.events || after.participants != before.participants {
		t.Fatalf("le groupe a changé : %+v → %+v", before, after)
	}
	for i := range after.rows {
		if after.rows[i] != before.rows[i] {
			t.Fatalf("paire modifiée : %+v → %+v", before.rows[i], after.rows[i])
		}
	}
	if len(e.mail.Sent) != 0 || len(e.archiver.calls) != 0 || len(e.remover.calls) != 0 {
		t.Fatal("courriel, archivage ou retrait malgré le refus")
	}
}

// F4c.AC5 : quand aucune réparation n'existe, l'aperçu l'explique et propose
// un tirage complet ; l'application est refusée sans rien modifier.
func TestAddImpossibleIsRefusedWithFullDrawProposal(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	w := e.add(t, gid, "Nouveau", normal)
	// Personne ne peut offrir à w.
	for _, id := range ids {
		if _, err := e.rules.AddExclusion(ctx, gid, id, w, false); err != nil {
			t.Fatal(err)
		}
	}
	before := e.snapshot(t, gid)

	p := e.preview(t, gid, addP(w))
	if p.Possible || p.Refusal == nil {
		t.Fatalf("refus attendu : %+v", p)
	}
	if !p.Refusal.ProposeFullDraw || p.Refusal.Capacity {
		t.Fatalf("proposition de tirage complet attendue : %+v", p.Refusal)
	}
	if p.Refusal.Message == "" || p.Summary == "" {
		t.Fatal("explication absente")
	}
	if !containsString(p.Refusal.Suggestions, msgFullDraw) {
		t.Fatalf("propositions sans tirage complet : %v", p.Refusal.Suggestions)
	}
	e.noNames(t, p.Summary)

	_, err := e.svc.Apply(ctx, admin(gid), gid, addP(w), Confirmation{Confirmed: true, Revision: p.Revision})
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("refus attendu, obtenu %v", err)
	}
	if code, _ := errs.CodeOf(err); code != CodeImpossible {
		t.Fatalf("code %q, attendu %q", code, CodeImpossible)
	}
	var ref *Refusal
	if !errors.As(err, &ref) || !ref.ProposeFullDraw {
		t.Fatalf("le refus doit porter la proposition de tirage complet : %v", err)
	}
	e.unchanged(t, gid, before)
}

// F4c.AC5 : un retrait sans réparation possible (forçage qui ne peut plus
// être tenu) est refusé de la même façon.
func TestRemoveImpossibleIsRefused(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	a, b, x := ids[0], ids[1], ids[2]
	e.setPairs(t, gid, ring(a, b, x))
	// Sans x, il ne reste que a et b : a→b et b→a. L'exclusion b→a l'interdit.
	if _, err := e.rules.AddExclusion(ctx, gid, b, a, false); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(t, gid)
	p := e.preview(t, gid, remove(x))
	if p.Possible || p.Refusal == nil || !p.Refusal.ProposeFullDraw {
		t.Fatalf("refus avec proposition attendu : %+v", p)
	}
	_, err := e.svc.Apply(ctx, admin(gid), gid, remove(x), Confirmation{Confirmed: true, Revision: p.Revision})
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("refus attendu, obtenu %v", err)
	}
	e.unchanged(t, gid, before)
}

// Sans tirage, il n'y a rien à ajuster.
func TestAdjustWithoutDraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal)
	_, err := e.svc.Preview(ctx, admin(gid), gid, remove(ids[0]))
	wantKind(t, err, errs.KindConflict)
	if e.count(t, `SELECT COUNT(*) FROM draws`) != 0 {
		t.Fatal("aucun tirage ne doit être créé")
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
