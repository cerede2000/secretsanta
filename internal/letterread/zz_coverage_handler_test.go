package letterread

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"secretsanta/internal/db"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

func TestZZImageHandler(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "lecture")
	ids := e.addN(t, gid, "Amandine", 4, participants.RoleNormal)
	e.write(t, gid, ids...)
	idea, err := e.letters.AddIdea(ctx, gid, ids[0], letter.IdeaInput{Title: "Avec image", Image: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	ideaID := idea
	e.mustDraw(t, gid)
	e.openReveal()
	pair := e.pairOf(t, gid, ids[0])
	e.discover(t, gid, pair.Giver, pair.ID)

	ms, err := websec.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(e.db, websec.MigrationModule, ms); err != nil {
		t.Fatal(err)
	}
	pol, err := websec.NewPolicy("https://noel.example.org")
	if err != nil {
		t.Fatal(err)
	}
	sess := websec.NewSessions(e.db, e.clock, pol, websec.SessionOptions{})
	mux := http.NewServeMux()
	for _, r := range NewHandler(e.svc, e.groups).Routes() {
		mux.Handle(r.Method+" "+r.Pattern, sess.Middleware(r.Handler))
	}
	login := func(id string) *http.Cookie {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if _, err := sess.Create(ctx, rec, req, websec.Identity{Kind: websec.KindParticipant, GroupID: strconv.FormatInt(gid, 10), Subject: id}); err != nil {
			t.Fatal(err)
		}
		return rec.Result().Cookies()[0]
	}
	get := func(method, path string, ck *http.Cookie) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		if ck != nil {
			req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
		}
		mux.ServeHTTP(rec, req)
		return rec
	}
	path := "/lecture/lettre-recue/" + strconv.FormatInt(pair.ID, 10) + "/image/" + strconv.FormatInt(ideaID, 10)

	rec := get(http.MethodGet, path, login(strconv.FormatInt(pair.Giver, 10)))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() == 0 {
		t.Fatalf("lecture : %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(http.MethodHead, path, login(strconv.FormatInt(pair.Giver, 10))); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD : %d %d", rec.Code, rec.Body.Len())
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"sans-session":   get(http.MethodGet, path, nil),
		"groupe-inconnu": get(http.MethodGet, "/inconnu/lettre-recue/1/image/1", nil),
		"sujet-invalide": get(http.MethodGet, path, login("abc")),
		"cible-invalide": get(http.MethodGet, "/lecture/lettre-recue/x/image/1", login(strconv.FormatInt(pair.Giver, 10))),
		"autre-tireur":   get(http.MethodGet, path, login(strconv.FormatInt(ids[0], 10))),
	} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s : %d", name, rec.Code)
		}
	}
	// Panne : base fermée, le service échoue (erreur interne).
	_ = e.db.Close()
	a := who(gid, pair.Giver)
	if _, err := e.svc.Read(ctx, a, gid, pair.ID); err == nil {
		t.Error("Read sur base fermée")
	}
	if _, _, err := e.svc.Image(ctx, a, gid, pair.ID, ideaID); err == nil {
		t.Error("Image sur base fermée")
	}
	rec = httptest.NewRecorder()
	(&Handler{}).fail(rec, errs.Internal(errors.New("panne")))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("500 attendu : %d", rec.Code)
	}
}

func TestZZHandlerFailNeutral(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.fail(rec, nil)
	if rec.Code != 404 {
		t.Fatalf("404 attendu : %d", rec.Code)
	}
}
