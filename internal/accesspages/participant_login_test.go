package accesspages

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/websec"
)

func TestCodeLoginOpensParticipantSession(t *testing.T) {
	e := newEnv(t)
	g := e.createGroup(t, "noel")
	gid := itoa(g.Group.ID)
	code, err := e.creds.IssueCode(context.Background(), gid, "42")
	if err != nil {
		t.Fatal(err)
	}
	rec := e.do(t, http.MethodPost, "/noel/connexion/code", url.Values{"code": {strings.ToLower(code)}}, "198.51.100.2", "fr")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/participant" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if id := e.sessions.opened[0]; id.Kind != websec.KindParticipant || id.Subject != "42" || id.GroupID != gid {
		t.Fatalf("identité : %+v", id)
	}
	bad := e.do(t, http.MethodPost, "/noel/connexion/code", url.Values{"code": {"ZZZZZZZZ"}}, "198.51.100.2", "fr")
	if bad.Code != http.StatusUnauthorized || !strings.Contains(bad.Body.String(), "ne correspond") {
		t.Fatalf("refus attendu : %d", bad.Code)
	}
}

func TestCodeLoginIsLimited(t *testing.T) {
	e := newEnv(t)
	e.createGroup(t, "noel")
	for i := 0; i < limiter.CodeAttempts.Limit; i++ {
		e.do(t, http.MethodPost, "/noel/connexion/code", url.Values{"code": {"ZZZZZZZZ"}}, "198.51.100.3", "fr")
	}
	if rec := e.do(t, http.MethodPost, "/noel/connexion/code", url.Values{"code": {"ZZZZZZZZ"}}, "198.51.100.3", "fr"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("429 attendu : %d", rec.Code)
	}
}

func TestMagicLinkConfirmOnGetConsumeOnPost(t *testing.T) {
	e := newEnv(t)
	e.createGroup(t, "noel")
	e.magic.valid["tok123"] = 7
	get := e.do(t, http.MethodGet, "/noel/connexion/lien/tok123", nil, "198.51.100.4", "fr")
	if get.Code != 200 || !strings.Contains(get.Body.String(), `method="post"`) {
		t.Fatalf("confirmation attendue : %d", get.Code)
	}
	if _, still := e.magic.valid["tok123"]; !still || e.sessions.count() != 0 {
		t.Fatal("le GET ne doit rien consommer")
	}
	post := e.do(t, http.MethodPost, "/noel/connexion/lien/tok123", nil, "198.51.100.4", "fr")
	if post.Code != http.StatusSeeOther || e.sessions.count() != 1 || e.sessions.opened[0].Subject != "7" {
		t.Fatalf("%d", post.Code)
	}
	again := e.do(t, http.MethodPost, "/noel/connexion/lien/tok123", nil, "198.51.100.4", "fr")
	if again.Code != http.StatusUnauthorized || e.sessions.count() != 1 {
		t.Fatalf("second usage : %d", again.Code)
	}
}

func TestPersonalLinkConfirmOnGet(t *testing.T) {
	e := newEnv(t)
	g := e.createGroup(t, "noel")
	tok, err := e.creds.IssueLink(context.Background(), itoa(g.Group.ID), "9")
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.do(t, http.MethodGet, "/noel/acces/"+tok, nil, "198.51.100.5", "fr"); rec.Code != 200 || e.sessions.count() != 0 {
		t.Fatalf("GET : %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/noel/acces/"+tok, nil, "198.51.100.5", "fr"); rec.Code != http.StatusSeeOther || e.sessions.opened[0].Subject != "9" {
		t.Fatalf("POST : %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/noel/acces/mauvais", nil, "198.51.100.5", "fr"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("jeton faux : %d", rec.Code)
	}
}

func TestMagicRequestIsUniformForUnknownGroup(t *testing.T) {
	e := newEnv(t)
	e.createGroup(t, "noel")
	a := e.do(t, http.MethodPost, "/noel/connexion/lien-magique", url.Values{"email": {"a@exemple.fr"}}, "198.51.100.6", "fr")
	b := e.do(t, http.MethodPost, "/inconnu/connexion/lien-magique", url.Values{"email": {"a@exemple.fr"}}, "198.51.100.6", "fr")
	if a.Code != 200 || b.Code != 200 || !strings.Contains(a.Body.String(), "un mail vient de partir") {
		t.Fatalf("%d %d", a.Code, b.Code)
	}
	if rec := e.do(t, http.MethodPost, "/noel/connexion/lien-magique", url.Values{"email": {"pasdemail"}}, "198.51.100.6", "fr"); rec.Code != http.StatusBadRequest {
		t.Fatalf("adresse invalide : %d", rec.Code)
	}
	e.magic.limited = true
	if rec := e.do(t, http.MethodPost, "/noel/connexion/lien-magique", url.Values{"email": {"a@exemple.fr"}}, "198.51.100.6", "fr"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("429 attendu : %d", rec.Code)
	}
}
