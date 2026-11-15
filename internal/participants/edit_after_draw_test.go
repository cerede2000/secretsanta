package participants

import (
	"bytes"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestEditAfterDraw(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	other := e.add(t, "C", "D")
	link, code := []byte("lien-condense-0123456789abcdef!!"), []byte("code-condense")
	if _, err := e.db.Exec(`UPDATE participants SET link_hash = ?, code_hash = ? WHERE id = ?`, link, code, p.ID); err != nil {
		t.Fatal(err)
	}
	e.draw.Done[e.g1] = true

	got, err := e.svc.Update(t.Context(), e.admin(), e.g1, p.ID, Input{FirstName: "Anna", Name: "Bé", Email: "n@x.fr", GiftHint: "thé"})
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstName != "Anna" || got.Name != "Bé" || got.Email != "n@x.fr" || got.GiftHint != "thé" {
		t.Fatalf("%+v", got)
	}
	var l2, c2 []byte
	if err := e.db.QueryRow(`SELECT link_hash, code_hash FROM participants WHERE id = ?`, p.ID).Scan(&l2, &c2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(l2, link) || !bytes.Equal(c2, code) {
		t.Fatal("lien et code doivent rester identiques")
	}
	// Unicité maintenue.
	_, err = e.svc.Update(t.Context(), e.admin(), e.g1, p.ID, Input{FirstName: "c", Name: "d"})
	wantKind(t, err, errs.KindConflict)
	// Garder son propre nom (autre casse) reste possible.
	if _, err := e.svc.Update(t.Context(), e.admin(), e.g1, other.ID, Input{FirstName: "CÉDRIC", Name: "D"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Update(t.Context(), e.admin(), e.g1, other.ID, Input{FirstName: "cedric", Name: "d"}); err != nil {
		t.Fatal(err)
	}
}
