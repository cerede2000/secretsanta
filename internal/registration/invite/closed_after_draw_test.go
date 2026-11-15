package invite

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F11.AC4 : dès que DrawState indique que le tirage est fait, toute nouvelle
// inscription et toute vérification d'inscription en attente sont refusées
// avec un renvoi vers l'ajustement du tirage.

func TestDrawDoneClosesRegistrations(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	h := e.server(t)
	page := get(h, l.Path) // formulaire ouvert avant le tirage
	if r := e.submit(t, "noel", l.Token, sub("Anne", "Lebrun", "anne@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("avant tirage : %+v", r)
	}
	if err := e.svc.VerificationAllowed(t.Context(), e.g1); err != nil {
		t.Fatalf("vérification refusée avant tirage : %v", err)
	}

	e.draw.Done[e.g1] = true

	inv, err := e.svc.Open(t.Context(), "noel", l.Token)
	if err != nil || inv.Closed != ClosedDraw || !inv.Closed.AdjustDrawHint() {
		t.Fatalf("fermeture au tirage : %+v %v", inv, err)
	}
	r := e.submit(t, "noel", l.Token, sub("Jean", "Petit", "jean@example.org"))
	if r.Outcome != OutcomeClosed || r.Closed != ClosedDraw || r.Problem.Code != "registration.closed_draw" ||
		!strings.Contains(r.Problem.Message, "Ajuster le tirage") {
		t.Fatalf("refus avec renvoi attendu : %+v", r)
	}
	if e.pendingCount(t, e.g1) != 1 || e.codes.count() != 1 {
		t.Fatal("inscription créée après le tirage")
	}

	// Vérification d'une inscription en attente : refusée, même renvoi.
	err = e.svc.VerificationAllowed(t.Context(), e.g1)
	if !errs.Is(err, errs.KindForbidden) || !strings.Contains(errs.UserMessage(err), "Ajuster le tirage") {
		t.Fatalf("vérification après tirage : %v", err)
	}
	if code, _ := errs.CodeOf(err); code != "registration.closed_draw" {
		t.Fatalf("code : %q", code)
	}
	// L'autre groupe n'est pas touché.
	if err := e.svc.VerificationAllowed(t.Context(), e.g2); err != nil {
		t.Fatal(err)
	}

	// Page : message de clôture avec le renvoi, sans formulaire ; un
	// formulaire ouvert avant le tirage ne crée rien.
	rec := get(h, l.Path)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Ajuster le tirage") ||
		strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("page close attendue : %d %s", rec.Code, rec.Body.String())
	}
	m := csrfRe.FindStringSubmatch(page.Body.String())
	form := formOf("Lou", "Roux", "lou@example.org")
	form.Set("csrf_token", html.UnescapeString(m[1]))
	post := rawPost(h, l.Path, form, page.Result().Cookies(), true)
	if post.Code != http.StatusOK || !strings.Contains(post.Body.String(), "Ajuster le tirage") {
		t.Fatalf("soumission après tirage : %d %s", post.Code, post.Body.String())
	}
	if e.pendingCount(t, e.g1) != 1 || e.codes.count() != 1 {
		t.Fatal("inscription créée après le tirage (HTTP)")
	}
}

func TestDrawClosurePrevailsOverOtherReasons(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	past := t0.Add(-1)
	if err := e.svc.SetLimits(t.Context(), admin(e.g1), e.g1, 1, &past); err != nil {
		t.Fatal(err)
	}
	e.addParticipant(t, e.g1, "Paul", "Durand", "")
	e.draw.Done[e.g1] = true
	inv, _ := e.svc.Open(t.Context(), "noel", l.Token)
	if inv.Closed != ClosedDraw {
		t.Fatalf("le renvoi vers l'ajustement doit primer : %q", inv.Closed)
	}
}
