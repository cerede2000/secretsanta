package app

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/config"
	"secretsanta/internal/participants"
)

// newNoSMTP construit un groupe « noel » sur une application SANS SMTP.
func newNoSMTP(t *testing.T) *w9 {
	t.Helper()
	a := newAppCfg(t, func(*config.Config) {})
	secret := createGroup(t, newBrowser(t, a), "noel", "Famille Noël")
	g, err := a.Groups.BySlug(context.Background(), "noel")
	if err != nil {
		t.Fatal(err)
	}
	return &w9{t: t, a: a, gid: g.ID, admin: adminLogin(t, a, "noel", secret), actor: participants.AdminActor(g.ID), secret: secret}
}

// Sans SMTP : l'invité lit la cause (jamais « Un problème est survenu ») et
// l'organisateur est averti sur son tableau de bord et sur la page des
// inscriptions.
func TestNoSMTPGuestGetsClearMessageAndAdminIsWarned(t *testing.T) {
	e := newNoSMTP(t)
	link := e.enableLink()

	guest := newBrowser(t, e.a)
	rec := guest.post(link, link, url.Values{"prenom": {"Léa"}, "nom": {"Martin"}, "email": {"lea@example.org"}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict {
		t.Fatalf("statut %d, 409 attendu", rec.Code)
	}
	if !strings.Contains(body, "Contacte l'organisateur") || strings.Contains(body, "Un problème est survenu") {
		t.Errorf("message invité incorrect :\n%s", body)
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM mail_queue`); n != 0 {
		t.Errorf("%d courriel(s) en file malgré l'envoi désactivé", n)
	}

	for _, p := range []string{"/noel/admin", "/noel/admin/inscriptions"} {
		pg := e.admin.get(p)
		if pg.Code != http.StatusOK || !strings.Contains(pg.Body.String(), "data-mail-disabled") {
			t.Errorf("%s : avertissement d'envoi absent (%d)", p, pg.Code)
		}
	}
}

// Avec SMTP, aucun avertissement.
func TestWithSMTPAdminIsNotWarned(t *testing.T) {
	e := newW9(t)
	for _, p := range []string{"/noel/admin", "/noel/admin/inscriptions"} {
		if pg := e.admin.get(p); strings.Contains(pg.Body.String(), "data-mail-disabled") {
			t.Errorf("%s : avertissement affiché alors que le SMTP est configuré", p)
		}
	}
}
