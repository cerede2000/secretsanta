package rulechange

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// recordingRepair note ce que le service transmet au port.
type recordingRepair struct {
	ports.FakeRuleChangeRepair
	previewed []ports.ProposedRule
	applied   []ports.ProposedRule
	applyErr  error
	prevErr   error
	prev      ports.RepairPreview
}

func (r *recordingRepair) Preview(_ context.Context, _ int64, rule ports.ProposedRule) (ports.RepairPreview, error) {
	r.previewed = append(r.previewed, rule)
	return r.prev, r.prevErr
}

func (r *recordingRepair) Apply(ctx context.Context, _ int64, rule ports.ProposedRule, w ports.RuleWriter) error {
	r.applied = append(r.applied, rule)
	if r.applyErr != nil {
		return r.applyErr
	}
	return w(ctx, nil)
}

func withRecorder(t *testing.T, e *env, r *recordingRepair) {
	t.Helper()
	e.svc = NewService(e.db, e.svc.clock, e.draw, r)
}

// Le port reçoit la nature « role » et la seule personne concernée.
func TestRoleChangeSendsOnlyKindAndPerson(t *testing.T) {
	e := newEnv(t, true)
	r := &recordingRepair{prev: ports.RepairPreview{AffectedCount: 1, Repairable: true}}
	withRecorder(t, e, r)
	if _, err := e.svc.SetRole(t.Context(), e.gid, e.ids[3], participants.RoleReceptionSeule, ChoiceAsk); err != nil {
		t.Fatal(err)
	}
	if len(r.previewed) != 1 || r.previewed[0].Kind != ports.RuleRoleChange ||
		len(r.previewed[0].Members) != 1 || r.previewed[0].Members[0] != e.ids[3] {
		t.Fatalf("aperçu demandé : %+v", r.previewed)
	}
	if len(r.applied) != 0 || e.role(t, e.ids[3]) != "normal" {
		t.Fatal("ni réparation ni écriture avant confirmation")
	}
}

// Un échec d'aperçu n'écrit rien et ne fuit rien.
func TestRolePreviewFailureWritesNothing(t *testing.T) {
	e := newEnv(t, true)
	withRecorder(t, e, &recordingRepair{prevErr: errors.New("paire Alice vers Bruno introuvable")})
	_, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], participants.RoleOffrePlus, ChoiceRepair)
	if err == nil || errs.KindOf(err) != errs.KindInternal {
		t.Fatalf("erreur interne attendue : %v", err)
	}
	noNames(t, msgOf(err))
	if e.role(t, e.ids[0]) != "normal" {
		t.Fatal("rôle écrit malgré l'échec")
	}
	if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
		t.Fatalf("indicateur inattendu : %v", f)
	}
}

// Un échec de réparation est signalé sans le texte brut du port.
func TestRoleApplyFailureMessageHasNoLeak(t *testing.T) {
	e := newEnv(t, true)
	withRecorder(t, e, &recordingRepair{
		prev:     ports.RepairPreview{AffectedCount: 1, Repairable: true},
		applyErr: errors.New("Alice offre à Bruno"),
	})
	o, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], participants.RoleOffrePlus, ChoiceRepair)
	if err == nil || o.Repaired {
		t.Fatalf("échec attendu : %+v %v", o, err)
	}
	noNames(t, msgOf(err))
}

// Aucune paire concernée : même un refus explicite ne pose pas d'indicateur.
func TestRoleZeroAffectedNeverFlags(t *testing.T) {
	e := newEnv(t, true)
	withRecorder(t, e, &recordingRepair{prev: ports.RepairPreview{AffectedCount: 0, Repairable: true}})
	o, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], participants.RoleOffrePlus, ChoiceFutureOnly)
	if err != nil || o.Status != StatusSaved {
		t.Fatalf("%+v %v", o, err)
	}
	if f, _ := e.svc.Flags(t.Context(), e.gid); len(f) != 0 {
		t.Fatalf("indicateur inattendu : %v", f)
	}
}

// Une personne inconnue ou d'un autre groupe est refusée sans rien écrire.
func TestRoleUnknownOrForeignParticipant(t *testing.T) {
	e := newEnv(t, true)
	res, err := e.db.Exec(`INSERT INTO groups(name, slug, admin_secret_hash, created_at) VALUES ('H','groupe-deux', zeroblob(32), '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := res.LastInsertId()
	for _, gid := range []int64{e.gid, other} {
		id := int64(9999)
		if gid == other {
			id = e.ids[0]
		}
		if _, err := e.svc.SetRole(t.Context(), gid, id, participants.RoleOffrePlus, ChoiceRepair); errs.KindOf(err) != errs.KindNotFound {
			t.Fatalf("groupe %d : non trouvé attendu, obtenu %v", gid, err)
		}
	}
	if e.role(t, e.ids[0]) != "normal" || e.repair.RepairCalls != 0 {
		t.Fatal("rien ne doit être écrit")
	}
}

// Un rôle forgé (casse, espaces, injection, vide) est refusé avant tout accès au port.
func TestRoleForgedValuesRefused(t *testing.T) {
	e := newEnv(t, true)
	r := &recordingRepair{}
	withRecorder(t, e, r)
	for _, v := range []participants.DrawRole{"", "NORMAL", " normal", "admin", "x' OR 1=1 --"} {
		if _, err := e.svc.SetRole(t.Context(), e.gid, e.ids[0], v, ChoiceRepair); errs.KindOf(err) != errs.KindValidation {
			t.Fatalf("rôle %q : refus de validation attendu, obtenu %v", v, err)
		}
	}
	if len(r.previewed)+len(r.applied) != 0 || e.role(t, e.ids[0]) != "normal" {
		t.Fatal("le port ne doit pas être appelé pour un rôle invalide")
	}
}

// Les quatre rôles valides s'écrivent, y compris un_seul, et l'écriture
// concerne uniquement la personne visée.
func TestRoleWritesOnlyTheTargetedRow(t *testing.T) {
	e := newEnv(t, false)
	for _, r := range []participants.DrawRole{participants.RoleReceptionSeule, participants.RoleOffrePlus, participants.RoleUnSeul, participants.RoleNormal} {
		if _, err := e.svc.SetRole(t.Context(), e.gid, e.ids[1], r, ChoiceAsk); err != nil {
			t.Fatalf("%s : %v", r, err)
		}
		if e.role(t, e.ids[1]) != string(r) {
			t.Fatalf("rôle %s non écrit", r)
		}
		for _, other := range []int64{e.ids[0], e.ids[2], e.ids[3]} {
			if e.role(t, other) != "normal" {
				t.Fatalf("le rôle de %d a changé", other)
			}
		}
	}
}

// Le service de rôle ne touche jamais aux tables du tirage : un changement de
// rôle ne peut ni réécrire une paire ni relancer un tirage en silence.
func TestRulechangeNeverTouchesDrawTables(t *testing.T) {
	src, err := os.ReadFile("rulechange.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"draw_pairs", "draw_events", "draws", "Redraw", "Draw("} {
		if strings.Contains(string(src), tbl) {
			t.Fatalf("rulechange.go mentionne %q", tbl)
		}
	}
}

func msgOf(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "(non typée) " + err.Error()
}
