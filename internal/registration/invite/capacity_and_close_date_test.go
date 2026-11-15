package invite

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/shared/errs"
)

// F11.AC3 : plafond (participants + inscriptions en attente non expirées) et
// date de clôture ferment les inscriptions avec un message clair pour un
// détenteur de jeton valide.

func TestCapacityCountsParticipantsAndPending(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, 3, nil); err != nil {
		t.Fatal(err)
	}
	e.addParticipant(t, e.g1, "Paul", "Durand", "paul@example.org")
	for i, n := range []string{"Martin", "Bernard"} {
		r := e.submit(t, "noel", l.Token, sub("Anne", n, fmt.Sprintf("anne%d@example.org", i)))
		if r.Outcome != OutcomeCodeSent {
			t.Fatalf("inscription %d : %+v", i, r)
		}
	}
	// 1 participant + 2 en attente = 3 places prises.
	inv, err := e.svc.Open(t.Context(), "noel", l.Token)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Closed != ClosedFull {
		t.Fatalf("plafond non appliqué : %+v", inv)
	}
	r := e.submit(t, "noel", l.Token, sub("Jean", "Petit", "jean@example.org"))
	if r.Outcome != OutcomeClosed || r.Closed != ClosedFull || r.Problem.Code != "registration.closed_full" ||
		!strings.Contains(r.Problem.Message, "closes") {
		t.Fatalf("refus attendu : %+v", r)
	}
	if e.pendingCount(t, e.g1) != 2 || e.codes.count() != 2 {
		t.Fatal("inscription créée au-delà du plafond")
	}
	// Le détenteur du jeton voit le message (page 200, pas la réponse neutre).
	rec := get(e.server(t), l.Path)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "toutes les places sont prises") ||
		strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("message clair attendu : %d %s", rec.Code, rec.Body.String())
	}

	// Les inscriptions expirées (48 h) ne comptent plus.
	e.clock.Advance(PendingTTL)
	inv, _ = e.svc.Open(t.Context(), "noel", l.Token)
	if inv.Closed != "" {
		t.Fatalf("inscriptions expirées comptées : %+v", inv)
	}
	if r := e.submit(t, "noel", l.Token, sub("Jean", "Petit", "jean@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("place libérée non rouverte : %+v", r)
	}

	// Sans plafond, aucune limite.
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, 0, nil); err != nil {
		t.Fatal(err)
	}
	if r := e.submit(t, "noel", l.Token, sub("Lou", "Roux", "lou@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("sans plafond : %+v", r)
	}
}

func TestCloseDateClosesRegistrations(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	closes := t0.Add(time.Hour)
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, 0, &closes); err != nil {
		t.Fatal(err)
	}
	st, _ := e.svc.Settings(t.Context(), admin(e.g1), e.g1)
	if st.ClosesAt == nil || !st.ClosesAt.Equal(closes) {
		t.Fatalf("date non enregistrée : %+v", st)
	}
	e.clock.Advance(time.Hour - time.Second)
	if inv, _ := e.svc.Open(t.Context(), "noel", l.Token); inv.Closed != "" {
		t.Fatalf("clos trop tôt : %+v", inv)
	}
	e.clock.Advance(time.Second)
	inv, err := e.svc.Open(t.Context(), "noel", l.Token)
	if err != nil || inv.Closed != ClosedDate {
		t.Fatalf("date de clôture ignorée : %+v %v", inv, err)
	}
	r := e.submit(t, "noel", l.Token, sub("Anne", "Lebrun", "anne@example.org"))
	if r.Outcome != OutcomeClosed || r.Closed != ClosedDate || r.Problem.Code != "registration.closed_date" {
		t.Fatalf("refus attendu : %+v", r)
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("inscription créée après la clôture")
	}
	rec := get(e.server(t), l.Path)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "date de clôture est passée") {
		t.Fatalf("message clair attendu : %d %s", rec.Code, rec.Body.String())
	}
	// Retirer la date rouvre.
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, 0, nil); err != nil {
		t.Fatal(err)
	}
	if inv, _ := e.svc.Open(t.Context(), "noel", l.Token); inv.Closed != "" {
		t.Fatalf("toujours clos : %+v", inv)
	}
}

func TestCapacityBounds(t *testing.T) {
	e := newEnv(t)
	for _, c := range []int{-1, MaxCapacity + 1} {
		if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, c, nil); !errs.Is(err, errs.KindValidation) {
			t.Fatalf("plafond %d accepté : %v", c, err)
		}
	}
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, MaxCapacity, nil); err != nil {
		t.Fatal(err)
	}
}
