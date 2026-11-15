package people

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

var (
	linkRe = regexp.MustCompile(`https://noel\.example\.org/noel/acces/([A-Za-z0-9_-]+)`)
	qrRe   = regexp.MustCompile(`src="(/noel/admin/participants/\d+/qr/[0-9a-f]+)"`)
)

func TestRegenerateInvalidatesOldLinkAndCodeAndShowsNewLinkAndQR(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.person(t, e.groupA, "Alice", "alice@exemple.fr", participants.RoleNormal)
	g, p := sid(e.groupA), sid(a)
	oldLink, err := e.tokens.IssueLink(ctx, g, p)
	if err != nil {
		t.Fatal(err)
	}
	oldCode, err := e.tokens.IssueCode(ctx, g, p)
	if err != nil {
		t.Fatal(err)
	}
	ck := e.adminCookies(t, e.groupA)

	// La confirmation intégrée ne régénère rien.
	ask := e.get(t, "/noel/admin/participants/"+p+"?regenerer=1", "fr", ck)
	if !strings.Contains(ask.Body.String(), "L&#39;ancien lien et l&#39;ancien code cessent de fonctionner") {
		t.Error("confirmation absente")
	}
	if got, err := e.tokens.ResolveLink(ctx, g, oldLink); err != nil || got != p {
		t.Fatalf("l'ancien lien devait encore valoir : %q %v", got, err)
	}

	rec := e.post(t, "/noel/admin/participants/"+p+"/regenerer", url.Values{}, "fr", ck)
	if rec.Code != 200 {
		t.Fatalf("200 attendu, %d", rec.Code)
	}
	body := rec.Body.String()
	m := linkRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("nouveau lien absent")
	}
	if m[1] == oldLink {
		t.Error("le lien n'a pas changé")
	}
	if _, err := e.tokens.ResolveLink(ctx, g, oldLink); err == nil {
		t.Error("l'ancien lien vaut encore")
	}
	if _, err := e.tokens.ResolveCode(ctx, g, oldCode); err == nil {
		t.Error("l'ancien code vaut encore")
	}
	if got, err := e.tokens.ResolveLink(ctx, g, m[1]); err != nil || got != p {
		t.Errorf("le nouveau lien devrait valoir : %q %v", got, err)
	}
	q := qrRe.FindStringSubmatch(body)
	if q == nil || strings.Contains(body, "data:") {
		t.Fatal("QR code servi par une route de même origine attendu")
	}
	img := e.get(t, q[1], "fr", ck)
	if img.Code != 200 || img.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(img.Body.String(), "\x89PNG") {
		t.Errorf("QR : %d %s", img.Code, img.Header().Get("Content-Type"))
	}
	if got := strings.Join(e.auditActions(t, e.groupA), ","); got != ActionLinkRegen {
		t.Errorf("journal : %q", got)
	}
	// Le QR n'est ni lisible sans session, ni depuis un autre groupe, ni pour une autre personne.
	if rec := e.get(t, q[1], "fr", nil); rec.Code != 303 {
		t.Errorf("QR sans session : 303 attendu, %d", rec.Code)
	}
	b := e.person(t, e.groupA, "Bruno", "bruno@exemple.fr", participants.RoleNormal)
	other := strings.Replace(q[1], "/participants/"+p+"/", "/participants/"+sid(b)+"/", 1)
	if rec := e.get(t, other, "fr", ck); rec.Code != 404 {
		t.Errorf("QR d'une autre personne : 404 attendu, %d", rec.Code)
	}
	// Une rechargement de la fiche ne remontre pas le lien.
	if strings.Contains(e.get(t, "/noel/admin/participants/"+p, "fr", ck).Body.String(), m[1]) {
		t.Error("le lien est resté affiché")
	}
}

func TestRegenerateWorksWithoutEmailAndStaysInsideTheGroup(t *testing.T) {
	e := newEnv(t)
	c := e.person(t, e.groupA, "Chloe", "", participants.RoleNormal)
	ck := e.adminCookies(t, e.groupA)
	rec := e.post(t, "/noel/admin/participants/"+sid(c)+"/regenerer", url.Values{}, "fr", ck)
	if rec.Code != 200 || linkRe.FindString(rec.Body.String()) == "" {
		t.Fatalf("lien attendu pour une personne sans courriel : %d", rec.Code)
	}
	if len(e.mail.items) != 0 {
		t.Error("aucun courriel ne doit partir")
	}
	other := e.adminCookies(t, e.groupB)
	if rec := e.post(t, "/paques/admin/participants/"+sid(c)+"/regenerer", url.Values{}, "fr", other); rec.Code != 404 {
		t.Errorf("autre groupe : 404 attendu, %d", rec.Code)
	}
}
