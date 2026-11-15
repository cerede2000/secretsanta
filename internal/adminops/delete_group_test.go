package adminops_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/shared/errs"
)

type twoGroups struct {
	f       *fixture
	g1, g2  int64
	before2 map[string]int
	img1    string
	img2    string
}

func seedTwoGroups(t *testing.T) *twoGroups {
	t.Helper()
	f := newFixture(t)
	g1, _ := f.group(t, "famille", "Noël en famille", "")
	g2, _ := f.group(t, "amis", "Amis", "")
	for _, g := range []int64{g1, g2} {
		a := f.person(t, g, "Anne", "Leroy", "", "")
		b := f.person(t, g, "Bob", "Roy", "", "")
		f.household(t, g, "Foyer", a, b)
		f.exec(t, `INSERT INTO manual_exclusions(group_id, giver_id, receiver_id) VALUES (?, ?, ?)`, g, a, b)
		f.seedDrawAndLetters(t, g, []int64{a, b})
		f.seedTraces(t, g, a)
	}
	s := &twoGroups{f: f, g1: g1, g2: g2}
	for _, g := range []int64{g1, g2} {
		dir := filepath.Join(f.images, strconv.FormatInt(g, 10))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "idee.png"), []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s.img1 = filepath.Join(f.images, strconv.FormatInt(g1, 10))
	s.img2 = filepath.Join(f.images, strconv.FormatInt(g2, 10))
	s.before2 = groupRowCounts(t, f.db, g2)
	return s
}

func (s *twoGroups) otherUntouched(t *testing.T) {
	t.Helper()
	if got := groupRowCounts(t, s.f.db, s.g2); !reflect.DeepEqual(got, s.before2) {
		t.Errorf("l'autre groupe a été touché :\n%v\n%v", got, s.before2)
	}
	if _, err := os.Stat(filepath.Join(s.img2, "idee.png")); err != nil {
		t.Errorf("images de l'autre groupe : %v", err)
	}
	if n := s.f.count(t, `SELECT COUNT(*) FROM household_members m JOIN households h ON h.id = m.household_id WHERE h.group_id = ?`, s.g2); n != 2 {
		t.Errorf("membres de foyer de l'autre groupe : %d", n)
	}
}

// F10b.AC8 : la suppression exige la confirmation par le nom du groupe.
func TestDeleteRequiresConfirmation(t *testing.T) {
	s := seedTwoGroups(t)
	before := groupRowCounts(t, s.f.db, s.g1)
	for _, confirm := range []string{"", "  ", "noël en famille", "Noël", "Amis"} {
		err := s.f.ops.DeleteGroup(context.Background(), s.g1, adminops.DeleteInput{ConfirmName: confirm, Purge: true})
		if code, _ := errs.CodeOf(err); code != adminops.CodeDeleteConfirm {
			t.Errorf("confirmation %q : %v", confirm, err)
		}
	}
	if got := groupRowCounts(t, s.f.db, s.g1); !reflect.DeepEqual(got, before) {
		t.Errorf("des données ont été supprimées sans confirmation : %v", got)
	}
	s.otherUntouched(t)
}

// F10b.AC8 : avec purge, plus aucune ligne ne porte l'identifiant du groupe
// et ses images sont effacées ; l'autre groupe est intact.
func TestDeleteWithPurgeErasesEverything(t *testing.T) {
	s := seedTwoGroups(t)
	if err := s.f.ops.DeleteGroup(context.Background(), s.g1, adminops.DeleteInput{ConfirmName: " Noël en famille ", Purge: true}); err != nil {
		t.Fatal(err)
	}
	for table, n := range groupRowCounts(t, s.f.db, s.g1) {
		if n != 0 {
			t.Errorf("%s : %d lignes restent après la purge", table, n)
		}
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM household_members m LEFT JOIN households h ON h.id = m.household_id WHERE h.id IS NULL`,
		`SELECT COUNT(*) FROM draw_pairs p LEFT JOIN draws d ON d.id = p.draw_id WHERE d.id IS NULL`,
		`SELECT COUNT(*) FROM letter_ideas i LEFT JOIN letters l ON l.id = i.letter_id WHERE l.id IS NULL`,
	} {
		if n := s.f.count(t, q); n != 0 {
			t.Errorf("orphelins : %s = %d", q, n)
		}
	}
	if _, err := os.Stat(s.img1); !os.IsNotExist(err) {
		t.Errorf("images du groupe non purgées : %v", err)
	}
	s.otherUntouched(t)
}

// Sans purge : le groupe, ses dépendances, ses accès et ses courriels en
// attente disparaissent ; l'historique des courriels envoyés, la liste de
// suppression et les désinscriptions restent.
func TestDeleteWithoutPurgeKeepsMailHistory(t *testing.T) {
	s := seedTwoGroups(t)
	if err := s.f.ops.DeleteGroup(context.Background(), s.g1, adminops.DeleteInput{ConfirmName: "Noël en famille"}); err != nil {
		t.Fatal(err)
	}
	left := groupRowCounts(t, s.f.db, s.g1)
	keep := map[string]int{"mail_queue": 1, "mail_suppression": 1, "mail_unsubscribe": 1}
	for table, n := range left {
		if n != keep[table] {
			t.Errorf("%s : %d lignes, %d attendues", table, n, keep[table])
		}
	}
	if n := s.f.count(t, `SELECT COUNT(*) FROM mail_queue WHERE group_id = ? AND status = 'pending'`, s.g1); n != 0 {
		t.Error("un courriel en attente partirait encore")
	}
	if _, err := os.Stat(s.img1); err != nil {
		t.Errorf("images supprimées sans purge : %v", err)
	}
	s.otherUntouched(t)
}
