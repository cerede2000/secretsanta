package adjustpages

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/adjust"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/drawengine"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/errs"
)

type errAdjust struct {
	previewErr error
	applyErr   error
}

func (f errAdjust) Preview(context.Context, participants.Actor, int64, adjust.Change) (*adjust.Preview, error) {
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	return &adjust.Preview{Possible: true, Revision: 3}, nil
}
func (f errAdjust) Apply(context.Context, participants.Actor, int64, adjust.Change, adjust.Confirmation) (*adjust.Result, error) {
	return nil, f.applyErr
}

type errMembership struct{}

func (errMembership) InDraw(context.Context, int64) (map[int64]bool, error) {
	return nil, errs.Internal(errors.New("lecture impossible"))
}

type errPeople struct{}

func (errPeople) List(context.Context, participants.Actor, int64) ([]participants.Participant, error) {
	return nil, errs.Forbidden("non")
}

func TestErrorKeyMapsEachKind(t *testing.T) {
	cases := []struct {
		err    error
		key    string
		status int
	}{
		{errs.Forbidden("x"), keyForbidden, http.StatusForbidden},
		{errs.New(errs.KindUnauthorized, "x"), keyForbidden, http.StatusForbidden},
		{errs.Conflict("x"), keyConflict, http.StatusConflict},
		{errs.Validation("x"), keyInvalid, http.StatusUnprocessableEntity},
		{errs.NotFound("x"), keyNotFound, http.StatusNotFound},
		{errors.New("autre"), keyInternal, http.StatusInternalServerError},
	}
	for _, c := range cases {
		if k, s := errorKey(c.err); k != c.key || s != c.status {
			t.Errorf("%v : %s/%d, attendu %s/%d", c.err, k, s, c.key, c.status)
		}
	}
}

func TestModeAndKindDefaults(t *testing.T) {
	if modeOf("n'importe quoi") != modeRemove || modeOf("") != modeRemove || modeOf(modeAdd) != modeAdd {
		t.Fatal("modeOf : seul « ajouter » sélectionne l'ajout")
	}
	if kindOf(modeAdd) != adjust.KindAdd || kindOf(modeRemove) != adjust.KindRemove || kindOf("x") != adjust.KindRemove {
		t.Fatal("kindOf : correspondance incorrecte")
	}
}

func TestExplainRefusalCauses(t *testing.T) {
	var d pageData
	explainRefusal(&d, nil)
	if !d.NoFix || d.CapCause != "" {
		t.Fatalf("refus nil : %+v", d)
	}
	for cause, want := range causeSuffix {
		d = pageData{}
		explainRefusal(&d, &adjust.Refusal{Cause: cause, Capacity: true, Missing: 2})
		if d.CapCause != want || !d.Capacity || d.Missing != 2 || !d.NoFix {
			t.Errorf("%v : %+v", cause, d)
		}
	}
	d = pageData{}
	explainRefusal(&d, &adjust.Refusal{Cause: drawengine.CauseSearchLimit})
	if d.CapCause != "other" {
		t.Fatalf("cause inconnue : %q", d.CapCause)
	}
}

func TestSQLMembershipInDrawReadsReceiversOfCurrentDrawOnly(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	// Schéma minimal : les tables lues par la requête et leurs dépendances.
	for _, s := range []struct {
		module string
		load   func() ([]db.Migration, error)
	}{{groups.Module, groups.Migrations}, {draw.Module, draw.Migrations}} {
		ms, err := s.load()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(d, s.module, ms); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.Exec(`INSERT INTO groups (slug, name, admin_secret_hash, created_at, max_gifts_per_giver) VALUES ('abc','A',zeroblob(32),?,2)`, now); err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`INSERT INTO draws (id, group_id, year, kind, status, gift_cap, drawn_at) VALUES (2,1,2025,'tirage','actuel',1,'` + now + `')`,
		`INSERT INTO draw_pairs (draw_id, giver_id, receiver_id) VALUES (2,10,99)`,
		`UPDATE draws SET status='archive', archived_at='` + now + `' WHERE id=2`,
		`INSERT INTO draws (id, group_id, year, kind, status, gift_cap, drawn_at) VALUES (1,1,2026,'tirage','actuel',1,'` + now + `')`,
		`INSERT INTO draw_pairs (draw_id, giver_id, receiver_id) VALUES (1,10,11),(1,11,12),(1,12,10)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := SQLMembership{DB: d}.InDraw(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got[10] || !got[11] || !got[12] || got[99] {
		t.Fatalf("appartenance : %v", got)
	}
	if none, err := (SQLMembership{DB: d}).InDraw(context.Background(), 42); err != nil || len(none) != 0 {
		t.Fatalf("groupe sans tirage : %v %v", none, err)
	}
	_ = d.Close()
	if _, err := (SQLMembership{DB: d}).InDraw(context.Background(), 1); errs.KindOf(err) != errs.KindInternal {
		t.Fatalf("base fermée : %v", err)
	}
}

func TestPageUnknownGroupIs404(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true})
	if rec := e.get("/inconnu/admin/ajuster", "fr", e.cookie()); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

func TestPageMembershipFailureRendersInternalError(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true})
	e.pages.o.Membership = errMembership{}
	rec := e.get("/noel/admin/ajuster?mode=retirer&personne=1", "fr", e.cookie())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("%d", rec.Code)
	}
}

func TestPagePeopleFailureKeepsStatusOfTheError(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true})
	e.pages.o.People = errPeople{}
	if rec := e.get("/noel/admin/ajuster", "fr", e.cookie()); rec.Code != http.StatusForbidden {
		t.Fatalf("%d", rec.Code)
	}
}

func TestPagePreviewErrorKeepsPageWithStatus(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	e.pages.o.Adjust = errAdjust{previewErr: errs.NotFound("personne")}
	rec := e.get("/noel/admin/ajuster?mode=retirer&personne=1", "fr", e.cookie())
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Zxalice") {
		t.Fatalf("%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `id="adj-prev"`) {
		t.Fatal("aucun aperçu ne doit être affiché après une erreur")
	}
}

func TestPageImpossiblePreviewExplainsRefusal(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	e.adj.preview = &adjust.Preview{Possible: false, Revision: 3,
		Refusal: &adjust.Refusal{Cause: drawengine.CauseCapTooLow, Capacity: true, Missing: 2, ProposeFullDraw: true}}
	rec := e.get("/noel/admin/ajuster?mode=retirer&personne=1", "fr", e.cookie())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/noel/admin/tirage") {
		t.Fatalf("%d", rec.Code)
	}
}

func TestPageRendersWithNavCallback(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true})
	called := ""
	e.pages.o.NavFor = func(_ *http.Request, active string) []adminnav.Item { called = active; return nil }
	if rec := e.get("/noel/admin/ajuster", "fr", e.cookie()); rec.Code != http.StatusOK || called != navKey {
		t.Fatalf("%d %q", rec.Code, called)
	}
}

func TestApplyRejectsBadInputs(t *testing.T) {
	e := newEnv(t, fakeMembership{1: true, 2: true})
	ck := e.cookie()
	page := "/noel/admin/ajuster?mode=retirer&personne=1"
	cases := []struct {
		name   string
		fields url.Values
		status int
	}{
		{"personne illisible", url.Values{"mode": {"retirer"}, "personne": {"abc"}, "revision": {"3"}, "confirm": {"1"}}, http.StatusUnprocessableEntity},
		{"révision illisible", url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"x"}, "confirm": {"1"}}, http.StatusUnprocessableEntity},
		{"identifiant nul", url.Values{"mode": {"retirer"}, "personne": {"0"}, "revision": {"3"}, "confirm": {"1"}}, http.StatusUnprocessableEntity},
		{"sans confirmation", url.Values{"mode": {"ajouter"}, "personne": {"3"}, "revision": {"3"}}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		if rec := e.post("/noel/admin/ajuster", page, ck, c.fields); rec.Code != c.status {
			t.Errorf("%s : %d", c.name, rec.Code)
		}
	}
	if len(e.adj.applied) != 0 || e.changed != 0 || len(e.audit.actions) != 0 {
		t.Fatal("une entrée invalide a modifié l'état")
	}
}

func TestApplyErrorsMapToPages(t *testing.T) {
	good := url.Values{"mode": {"retirer"}, "personne": {"1"}, "revision": {"3"}, "confirm": {"1"}}
	page := "/noel/admin/ajuster?mode=retirer&personne=1"
	t.Run("conflit signale un tirage changé", func(t *testing.T) {
		e := newEnv(t, fakeMembership{1: true, 2: true})
		e.pages.o.Adjust = errAdjust{applyErr: errs.Conflict("révision")}
		rec := e.post("/noel/admin/ajuster", page, e.cookie(), good)
		if rec.Code != http.StatusConflict || e.changed != 0 || len(e.audit.actions) != 0 {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("refus explique la cause", func(t *testing.T) {
		e := newEnv(t, fakeMembership{1: true, 2: true})
		e.pages.o.Adjust = errAdjust{applyErr: &adjust.Refusal{Cause: drawengine.CauseTooFewGivers, Capacity: true, Missing: 1}}
		rec := e.post("/noel/admin/ajuster", page, e.cookie(), good)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "/noel/admin/tirage") {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("refus avec liste illisible", func(t *testing.T) {
		e := newEnv(t, fakeMembership{1: true, 2: true})
		e.pages.o.Adjust = errAdjust{applyErr: &adjust.Refusal{Cause: drawengine.CauseSearchLimit}}
		rec := e.postBreaking(page, good, func() { e.pages.o.Membership = errMembership{} })
		if rec.Code != http.StatusConflict {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("erreur interne", func(t *testing.T) {
		e := newEnv(t, fakeMembership{1: true, 2: true})
		e.pages.o.Adjust = errAdjust{applyErr: errors.New("boum")}
		if rec := e.post("/noel/admin/ajuster", page, e.cookie(), good); rec.Code != http.StatusInternalServerError {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("succès avec liste illisible rend quand même le résultat", func(t *testing.T) {
		e := newEnv(t, fakeMembership{1: true, 2: true})
		rec := e.postBreaking(page, good, func() { e.pages.o.Membership = errMembership{} })
		if rec.Code != http.StatusOK || len(e.adj.applied) != 1 || e.changed != 1 {
			t.Fatalf("%d applied=%d changed=%d", rec.Code, len(e.adj.applied), e.changed)
		}
	})
}

// postBreaking prend le jeton CSRF sur la page, appelle brk, puis envoie le POST.
func (e *env) postBreaking(page string, fields url.Values, brk func()) *httptest.ResponseRecorder {
	e.t.Helper()
	ck := e.cookie()
	pg := e.get(page, "fr", ck)
	m := csrfRe.FindStringSubmatch(pg.Body.String())
	if m == nil {
		e.t.Fatal("jeton CSRF introuvable")
	}
	brk()
	form := url.Values{"csrf_token": {m[1]}}
	for k, v := range fields {
		form[k] = v
	}
	req := httptest.NewRequest(http.MethodPost, "/noel/admin/ajuster", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(ck)
	for _, c := range pg.Result().Cookies() {
		if c.Name != ck.Name {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}
