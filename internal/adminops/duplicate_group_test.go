package adminops_test

import (
	"context"
	"reflect"
	"testing"

	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
)

// F10b.AC7 : la duplication crée le groupe de l'année suivante avec
// participants, foyers, exclusions et réglages, sans tirage ni lettres.
func TestDuplicateCreatesNextYearGroup(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	src, srcSecret := f.group(t, "noel-2026", "Noël en famille 2026", "2026-12-24")
	f.exec(t, `UPDATE groups SET reveal_at = ?, admin_can_view_pair = 1, max_gifts_per_giver = 3 WHERE id = ?`, "2026-12-01T17:00:00Z", src)
	f.exec(t, `INSERT INTO group_settings(group_id, default_lang) VALUES (?, 'en')`, src)
	f.exec(t, `INSERT INTO reminder_group_rules(group_id, enabled, first_delay_days, spacing_days, max_per_type) VALUES (?, 0, 5, 6, 2)`, src)
	a := f.person(t, src, "Élodie", "Martin", "elodie@exemple.fr", "")
	b := f.person(t, src, "Paul", "Martin", "", "")
	c := f.person(t, src, "Zoé", "Durand", "", participants.RoleOffrePlus)
	d := f.person(t, src, "Hugo", "Petit", "", "")
	f.household(t, src, "Les Martin", a, b)
	f.exec(t, `INSERT INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?, ?, ?)`, src, c, d)
	f.exec(t, `INSERT INTO forced_pairs(group_id, giver_id, receiver_id) VALUES (?, ?, ?)`, src, d, a)
	f.seedDrawAndLetters(t, src, []int64{a, b, c, d})
	f.seedTraces(t, src, a)

	plan, err := f.ops.PlanDuplicate(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.ops.Duplicate(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "Noël en famille 2027" || res.Slug != "noel-2027" || res.Year != 2027 {
		t.Errorf("nouveau groupe : %+v", res)
	}
	if plan.Name != res.Name || plan.Slug != res.Slug || plan.Year != res.Year {
		t.Errorf("le plan %+v ne correspond pas au résultat", plan)
	}
	g, err := f.groups.ByID(ctx, res.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := f.groups.ByID(ctx, src)
	if g.EventDate != "2027-12-24" || g.RevealAt == nil || g.RevealAt.Format("2006-01-02T15:04") != "2027-12-01T17:00" {
		t.Errorf("dates : %q %v", g.EventDate, g.RevealAt)
	}
	if !reflect.DeepEqual(g.BudgetCents, old.BudgetCents) || g.GiftInstructions != old.GiftInstructions ||
		!g.AdminCanViewPair || g.MaxGiftsPerGiver != 3 || g.BudgetVersion != 0 {
		t.Errorf("réglages non copiés : %+v", g)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM group_settings WHERE group_id = ? AND default_lang = 'en'`, g.ID); n != 1 {
		t.Error("langue du groupe non copiée")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM reminder_group_rules WHERE group_id = ? AND enabled = 0 AND first_delay_days = 5 AND spacing_days = 6 AND max_per_type = 2`, g.ID); n != 1 {
		t.Error("relances non copiées")
	}
	// Mot de passe propre au nouveau groupe, affiché une seule fois.
	if id, err := f.groups.VerifyAdminSecret(ctx, "noel-2027", res.AdminSecret); err != nil || id != g.ID {
		t.Errorf("secret du nouveau groupe : %v", err)
	}
	if _, err := f.groups.VerifyAdminSecret(ctx, "noel-2027", srcSecret); err == nil {
		t.Error("l'ancien secret ouvre le nouveau groupe")
	}
	if got, want := roster(t, f.db, g.ID), roster(t, f.db, src); !reflect.DeepEqual(got, want) || len(got) != 4 {
		t.Errorf("participants : %v / %v", got, want)
	}
	if got, want := households(t, f.db, g.ID), households(t, f.db, src); !reflect.DeepEqual(got, want) || len(got) != 1 {
		t.Errorf("foyers : %v / %v", got, want)
	}
	for _, table := range []string{"manual_exclusions", "forced_pairs"} {
		if got, want := namedPairs(t, f.db, table, g.ID), namedPairs(t, f.db, table, src); !reflect.DeepEqual(got, want) || len(got) != 1 {
			t.Errorf("%s : %v / %v", table, got, want)
		}
	}
	// Ni tirage, ni lettres, ni accès personnels, ni courriels.
	for _, q := range []string{
		`SELECT COUNT(*) FROM draws WHERE group_id = ?`, `SELECT COUNT(*) FROM letters WHERE group_id = ?`,
		`SELECT COUNT(*) FROM draw_events WHERE group_id = ?`, `SELECT COUNT(*) FROM mail_queue WHERE group_id = ?`,
		`SELECT COUNT(*) FROM access_credentials WHERE group_id = CAST(? AS TEXT)`,
		`SELECT COUNT(*) FROM participants WHERE group_id = ? AND (link_hash IS NOT NULL OR code_hash IS NOT NULL)`,
	} {
		if n := f.count(t, q, g.ID); n != 0 {
			t.Errorf("%s : %d", q, n)
		}
	}
	// Le groupe d'origine est intact.
	if n := f.count(t, `SELECT COUNT(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id WHERE d.group_id = ?`, src); n != 4 {
		t.Errorf("tirage d'origine : %d paires", n)
	}
	// Une seconde duplication prend une autre adresse libre.
	again, err := f.ops.Duplicate(ctx, src)
	if err != nil || again.Slug != "noel-2027-2" {
		t.Errorf("seconde duplication : %+v %v", again, err)
	}
}

// Sans année dans le nom ni jour J, l'année courante sert de base.
func TestDuplicateWithoutYearAppendsNextYear(t *testing.T) {
	f := newFixture(t)
	src, _ := f.group(t, "famille", "Famille", "")
	res, err := f.ops.Duplicate(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "Famille 2027" || res.Slug != "famille-2027" {
		t.Errorf("%+v", res)
	}
	if _, err := groups.ValidateSlug(res.Slug); err != nil {
		t.Error(err)
	}
}
