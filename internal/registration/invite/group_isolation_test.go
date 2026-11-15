package invite

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"secretsanta/internal/shared/errs"
)

// F11.AC8 : un jeton d'invitation d'un groupe ne permet jamais de s'inscrire
// dans un autre groupe, et un administrateur ne gère que les inscriptions de
// son groupe.

func TestTokenNeverCrossesGroups(t *testing.T) {
	e := newEnv(t)
	l1 := e.enable(t, e.g1)
	l2 := e.enable(t, e.g2)
	for _, c := range []struct{ slug, tok string }{{"famille", l1.Token}, {"noel", l2.Token}} {
		if _, err := e.svc.Open(t.Context(), c.slug, c.tok); !errors.Is(err, ErrNeutral) {
			t.Fatalf("%s accepte un jeton étranger : %v", c.slug, err)
		}
		if _, err := e.svc.Submit(t.Context(), c.slug, c.tok, sub("Anne", "Lebrun", "anne@example.org")); !errors.Is(err, ErrNeutral) {
			t.Fatalf("%s : soumission avec un jeton étranger : %v", c.slug, err)
		}
	}
	h := e.server(t)
	if rec := get(h, "/famille/rejoindre/"+l1.Token); rec.Code != http.StatusNotFound {
		t.Fatalf("page : %d", rec.Code)
	}
	if e.pendingCount(t, e.g1)+e.pendingCount(t, e.g2) != 0 {
		t.Fatal("inscription créée avec un jeton étranger")
	}
	// Chaque jeton inscrit dans son groupe, et seulement là.
	if r := e.submit(t, "noel", l1.Token, sub("Anne", "Lebrun", "anne@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatal(r)
	}
	if e.pendingCount(t, e.g1) != 1 || e.pendingCount(t, e.g2) != 0 || e.codes.issued[0].GroupID != e.g1 {
		t.Fatal("inscription dans le mauvais groupe")
	}
	// Renouveler un groupe ne touche pas l'autre.
	if _, err := e.svc.Renew(t.Context(), admin(e.g1), e.g1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Open(t.Context(), "famille", l2.Token); err != nil {
		t.Fatalf("jeton de l'autre groupe invalidé : %v", err)
	}
}

func TestAdminManagesOnlyOwnGroup(t *testing.T) {
	e := newEnv(t)
	l2 := e.enable(t, e.g2)
	if r := e.submit(t, "famille", l2.Token, sub("Zoé", "Blanc", "zoe@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatal(r)
	}
	a1 := admin(e.g1)
	forbidden := func(name string, err error) {
		t.Helper()
		if !errs.Is(err, errs.KindForbidden) {
			t.Errorf("%s sur l'autre groupe : %v", name, err)
		}
	}
	_, err := e.svc.Enable(t.Context(), a1, e.g2)
	forbidden("Enable", err)
	_, err = e.svc.Renew(t.Context(), a1, e.g2)
	forbidden("Renew", err)
	forbidden("Disable", e.svc.Disable(t.Context(), a1, e.g2))
	when := t0.Add(time.Hour)
	forbidden("SetLimits", e.svc.SetLimits(t.Context(), a1, e.g2, 1, &when))
	_, err = e.svc.Settings(t.Context(), a1, e.g2)
	forbidden("Settings", err)
	_, err = e.svc.ListPending(t.Context(), a1, e.g2)
	forbidden("ListPending", err)

	// Rien n'a changé dans le groupe 2.
	if _, err := e.svc.Open(t.Context(), "famille", l2.Token); err != nil {
		t.Fatalf("groupe 2 modifié : %v", err)
	}
	st, _ := e.svc.Settings(t.Context(), admin(e.g2), e.g2)
	if st.Capacity != 0 || st.ClosesAt != nil {
		t.Fatalf("limites modifiées : %+v", st)
	}
	// L'administrateur 1 ne voit que ses inscriptions.
	mine, err := e.svc.ListPending(t.Context(), a1, e.g1)
	if err != nil || len(mine) != 0 {
		t.Fatalf("inscriptions du groupe 1 : %+v %v", mine, err)
	}
	theirs, err := e.svc.ListPending(t.Context(), admin(e.g2), e.g2)
	if err != nil || len(theirs) != 1 || theirs[0].FirstName != "Zoé" {
		t.Fatalf("inscriptions du groupe 2 : %+v %v", theirs, err)
	}
	// Le magasin refuse de supprimer l'inscription d'un autre groupe.
	if err := e.store.DeletePending(t.Context(), e.g1, theirs[0].ID); err != nil {
		t.Fatal(err)
	}
	if e.pendingCount(t, e.g2) != 1 {
		t.Fatal("inscription d'un autre groupe supprimée")
	}
}

func TestOtherGroupDoesNotCountTowardCapacity(t *testing.T) {
	e := newEnv(t)
	l1 := e.enable(t, e.g1)
	l2 := e.enable(t, e.g2)
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, 1, nil); err != nil {
		t.Fatal(err)
	}
	e.addParticipant(t, e.g2, "Paul", "Durand", "")
	if r := e.submit(t, "famille", l2.Token, sub("Zoé", "Blanc", "zoe@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatal(r)
	}
	if inv, _ := e.svc.Open(t.Context(), "noel", l1.Token); inv.Closed != "" {
		t.Fatalf("l'autre groupe remplit le plafond : %+v", inv)
	}
	e.draw.Done[e.g2] = true
	if inv, _ := e.svc.Open(t.Context(), "noel", l1.Token); inv.Closed != "" {
		t.Fatalf("le tirage de l'autre groupe ferme celui-ci : %+v", inv)
	}
}
