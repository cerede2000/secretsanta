package invite

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// F11.AC7 : une adresse déjà connue dans le groupe provoque la demande de
// confirmation d'adresse partagée ; confirmée, le code part à cette adresse ;
// non confirmée, aucun courriel n'est envoyé ; aucun nom n'est jamais cité.

func noNames(t *testing.T, r *Result, names ...string) {
	t.Helper()
	shown := *r
	shown.Values = Values{}
	dump := strings.ToLower(fmt.Sprintf("%+v", shown))
	for _, n := range names {
		if strings.Contains(dump, strings.ToLower(n)) {
			t.Fatalf("la réponse cite %q : %s", n, dump)
		}
	}
}

func TestSharedAddressWithParticipant(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "foyer@example.org")

	s := sub("Léa", "Durand", "Foyer@Example.org")
	r := e.submit(t, "noel", l.Token, s)
	if r.Outcome != OutcomeConfirmShared || r.Problem.Code != "registration.shared_address_confirm" ||
		!strings.Contains(r.Problem.Message, "Cette adresse est partagée avec un proche") {
		t.Fatalf("confirmation attendue : %+v", r)
	}
	noNames(t, r, "Paul")
	if r.Values.Email != "Foyer@Example.org" || r.Values.FirstName != "Léa" {
		t.Fatalf("saisie à reproposer : %+v", r.Values)
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("envoi sans confirmation")
	}

	s.ConfirmShared = true
	r = e.submit(t, "noel", l.Token, s)
	if r.Outcome != OutcomeCodeSent {
		t.Fatalf("confirmée : %+v", r)
	}
	noNames(t, r, "Paul")
	if e.codes.count() != 1 || e.codes.issued[0].Email != "Foyer@Example.org" || !e.codes.issued[0].SharedAddress {
		t.Fatalf("code non envoyé à l'adresse partagée : %+v", e.codes.issued)
	}
}

func TestSharedAddressWithPendingRegistration(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	if r := e.submit(t, "noel", l.Token, sub("Paul", "Durand", "foyer@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatal(r)
	}
	r := e.submit(t, "noel", l.Token, sub("Léa", "Durand", "FOYER@example.org"))
	if r.Outcome != OutcomeConfirmShared {
		t.Fatalf("adresse d'une inscription en attente non détectée : %+v", r)
	}
	noNames(t, r, "Paul")
	if e.codes.count() != 1 {
		t.Fatal("envoi sans confirmation")
	}
}

func TestUnknownAddressNeedsNoConfirmation(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "foyer@example.org")
	// Même adresse dans un autre groupe : inconnue ici.
	e.addParticipant(t, e.g2, "Zoé", "Blanc", "zoe@example.org")
	r := e.submit(t, "noel", l.Token, sub("Zoé", "Blanc", "zoe@example.org"))
	if r.Outcome != OutcomeCodeSent || e.codes.count() != 1 || e.codes.issued[0].SharedAddress {
		t.Fatalf("adresse inconnue : %+v %+v", r, e.codes.issued)
	}
}

func TestSharedAddressPage(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "foyer@example.org")
	h := e.server(t)
	rec := postForm(t, h, l.Path, formOf("Léa", "Durand", "foyer@example.org"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), string(OutcomeConfirmShared)) ||
		strings.Contains(rec.Body.String(), "Paul") {
		t.Fatalf("page : %d %s", rec.Code, rec.Body.String())
	}
	if e.codes.count() != 0 {
		t.Fatal("envoi sans confirmation (HTTP)")
	}
	f := formOf("Léa", "Durand", "foyer@example.org")
	f.Set(FieldConfirmShared, "1")
	rec = postForm(t, h, l.Path, f)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), string(OutcomeCodeSent)) || e.codes.count() != 1 {
		t.Fatalf("confirmation (HTTP) : %d %s", rec.Code, rec.Body.String())
	}
}
