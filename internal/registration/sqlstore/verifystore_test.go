package sqlstore

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"

	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/verify"
)

func sameOutcome(a, b verify.Outcome) bool {
	if !a.VerifiedAt.Equal(b.VerifiedAt) || !a.ExpiresAt.Equal(b.ExpiresAt) {
		return false
	}
	a.VerifiedAt, b.VerifiedAt, a.ExpiresAt, b.ExpiresAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return a == b
}

func sameAudit(a, b []verify.AuditEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].At.Equal(b[i].At) {
			return false
		}
		x, y := a[i], b[i]
		x.At, y.At = time.Time{}, time.Time{}
		if x != y {
			return false
		}
	}
	return true
}

// Propriété : sur toute suite d'opérations, le magasin SQL de l'état de
// vérification répond exactement comme verify.MemoryStore (traces, retrait par
// référence, expiration, inscriptions ignorées, journal, isolation des
// groupes).
func TestVerifyStoreMatchesMemoryStoreProperty(t *testing.T) {
	ctx := t.Context()
	d := openMigrated(t, t.TempDir())
	st := NewVerifyStore(d)
	gen := 0
	rapid.Check(t, func(rt *rapid.T) {
		gen++
		groups := []int64{insertGroup(t, d, fmt.Sprintf("v%da", gen)), insertGroup(t, d, fmt.Sprintf("v%db", gen))}
		ref := verify.NewMemoryStore()
		now := t0
		for i, n := 0, rapid.IntRange(1, 50).Draw(rt, "ops"); i < n; i++ {
			g := rapid.SampledFrom(groups).Draw(rt, "group")
			reg := int64(rapid.IntRange(1, 4).Draw(rt, "reg"))
			switch rapid.IntRange(0, 9).Draw(rt, "op") {
			case 0:
				o := verify.Outcome{
					GroupID: g, RegistrationID: reg,
					Ref:           rapid.SampledFrom([]string{"r1", "r2", "r3"}).Draw(rt, "ref"),
					ParticipantID: int64(rapid.IntRange(1, 9).Draw(rt, "pid")),
					VerifiedAt:    now, ByAdmin: rapid.Bool().Draw(rt, "admin"),
					ExpiresAt: now.Add(time.Duration(rapid.IntRange(-2, 48).Draw(rt, "ttl")) * time.Hour),
				}
				if err := st.SaveOutcome(ctx, o); err != nil {
					rt.Fatal(err)
				}
				_ = ref.SaveOutcome(ctx, o)
			case 1:
				got, ok, err := st.Outcome(ctx, g, reg)
				if err != nil {
					rt.Fatal(err)
				}
				want, wok, _ := ref.Outcome(ctx, g, reg)
				if ok != wok || (ok && !sameOutcome(got, want)) {
					rt.Fatalf("Outcome : SQL %+v/%v, référence %+v/%v", got, ok, want, wok)
				}
			case 2:
				r := rapid.SampledFrom([]string{"", "r1", "r2", "r3", "r9"}).Draw(rt, "lookup")
				got, ok, err := st.OutcomeByRef(ctx, g, r)
				if err != nil {
					rt.Fatal(err)
				}
				want, wok, _ := ref.OutcomeByRef(ctx, g, r)
				// Deux traces d'un même groupe ne partagent jamais une
				// référence (aléatoire) : on ne compare que l'unique cas.
				if ok != wok {
					rt.Fatalf("OutcomeByRef(%q) : SQL %v, référence %v", r, ok, wok)
				}
				if ok && got.Ref != want.Ref {
					rt.Fatalf("OutcomeByRef(%q) : références %q et %q", r, got.Ref, want.Ref)
				}
			case 3:
				if err := st.DeleteOutcome(ctx, g, reg); err != nil {
					rt.Fatal(err)
				}
				_ = ref.DeleteOutcome(ctx, g, reg)
			case 4:
				got, err := st.DeleteOutcomesExpired(ctx, g, now)
				if err != nil {
					rt.Fatal(err)
				}
				if want, _ := ref.DeleteOutcomesExpired(ctx, g, now); got != want {
					rt.Fatalf("DeleteOutcomesExpired : SQL %d, référence %d", got, want)
				}
			case 5:
				if err := st.SetIgnored(ctx, g, reg, now); err != nil {
					rt.Fatal(err)
				}
				_ = ref.SetIgnored(ctx, g, reg, now)
			case 6:
				if err := st.ClearIgnored(ctx, g, reg); err != nil {
					rt.Fatal(err)
				}
				_ = ref.ClearIgnored(ctx, g, reg)
			case 7:
				got, err := st.Ignored(ctx, g)
				if err != nil {
					rt.Fatal(err)
				}
				want, _ := ref.Ignored(ctx, g)
				if !reflect.DeepEqual(got, want) {
					rt.Fatalf("Ignored : SQL %v, référence %v", got, want)
				}
			case 8:
				e := verify.AuditEntry{
					GroupID: g, RegistrationID: reg,
					Action: rapid.SampledFrom([]verify.Action{verify.ActionValidate, verify.ActionResend, verify.ActionDelete}).Draw(rt, "action"),
					Actor:  "administrateur", At: now, FirstName: "Léa", Name: "Martin",
					ParticipantID: int64(rapid.IntRange(0, 3).Draw(rt, "audpid")),
				}
				if err := st.AppendAudit(ctx, e); err != nil {
					rt.Fatal(err)
				}
				_ = ref.AppendAudit(ctx, e)
			case 9:
				now = now.Add(time.Duration(rapid.IntRange(0, 30).Draw(rt, "hours")) * time.Hour)
				got, err := st.Audit(ctx, g)
				if err != nil {
					rt.Fatal(err)
				}
				want, _ := ref.Audit(ctx, g)
				if !sameAudit(got, want) {
					rt.Fatalf("Audit : SQL %+v, référence %+v", got, want)
				}
			}
		}
	})
}

// L'instant zéro désigne toutes les inscriptions, expirées comprises ; un
// instant réel ne garde que les vivantes (la purge au démarrage en dépend).
func TestListPendingZeroTimeReturnsExpiredToo(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l := e.enable(t, e.g1)
	e.submit(t, "noel", l.Token, submission("Léa", "Martin", "lea@example.org"))
	e.clock.Advance(invite.PendingTTL + time.Hour)
	if got, err := e.invites.ListPending(ctx, e.g1, e.clock.Now()); err != nil || len(got) != 0 {
		t.Fatalf("ListPending à l'instant courant : %d, %v, attendu 0", len(got), err)
	}
	got, err := e.invites.ListPending(ctx, e.g1, time.Time{})
	if err != nil || len(got) != 1 {
		t.Fatalf("ListPending à l'instant zéro : %d, %v, attendu 1 (expirée comprise)", len(got), err)
	}
	if n, err := e.invites.CountPending(ctx, e.g1, time.Time{}); err != nil || n != 1 {
		t.Fatalf("CountPending à l'instant zéro : %d, %v", n, err)
	}
	if got, err := e.invites.ListPending(ctx, e.g2, time.Time{}); err != nil || len(got) != 0 {
		t.Fatalf("autre groupe : %d, %v", len(got), err)
	}
}

// La migration 002 crée les trois tables d'état, rattachées aux groupes en
// cascade, sans aucune colonne de secret ni de paire.
func TestVerifyStateTablesAndGroupCascade(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	want := map[string][]string{
		"registration_outcomes": {"group_id", "registration_id", "ref", "participant_id", "verified_at", "by_admin", "expires_at"},
		"registration_ignored":  {"group_id", "registration_id", "ignored_at"},
		"registration_audit":    {"id", "group_id", "registration_id", "action", "actor", "at", "first_name", "name", "participant_id"},
	}
	for table, cols := range want {
		if got := columns(t, e.db, table); !reflect.DeepEqual(got, cols) {
			t.Fatalf("%s : colonnes %v, attendu %v", table, got, cols)
		}
		var n int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE "table" = 'groups' AND on_delete = 'CASCADE'`,
			table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s : clé étrangère vers groups en cascade absente (%d, %v)", table, n, err)
		}
	}
	st := NewVerifyStore(e.db)
	if err := st.SaveOutcome(ctx, verify.Outcome{GroupID: e.g1, RegistrationID: 1, Ref: "r", ParticipantID: 1, VerifiedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetIgnored(ctx, e.g1, 2, t0); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendAudit(ctx, verify.AuditEntry{GroupID: e.g1, RegistrationID: 1, Action: verify.ActionDelete, Actor: "administrateur", At: t0, FirstName: "A", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	ids, err := st.GroupIDs(ctx)
	if err != nil || !reflect.DeepEqual(ids, []int64{e.g1}) {
		t.Fatalf("GroupIDs = %v, %v", ids, err)
	}
	if _, err := e.db.Exec(`DELETE FROM groups WHERE id = ?`, e.g1); err != nil {
		t.Fatal(err)
	}
	for table := range want {
		if n := countRows(t, e.db, table); n != 0 {
			t.Fatalf("%s : %d lignes survivent à la suppression du groupe", table, n)
		}
	}
}
