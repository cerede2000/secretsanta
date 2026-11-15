package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestEnableShowsLinkAndQRFromSameOrigin(t *testing.T) {
	e := newEnv(t)
	w := e.post(t, "famille", "/famille/admin/inscriptions/mode", url.Values{"enabled": {"1"}})
	if w.Code != 303 {
		t.Fatalf("statut %d", w.Code)
	}
	page := e.do("GET", w.Header().Get("Location"), "fr", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(page, testOrigin+"/famille/rejoindre/tok1") {
		t.Error("lien absent")
	}
	if !strings.Contains(page, `src="/famille/admin/inscriptions/qr.png"`) {
		t.Error("QR absent")
	}
	q := e.do("GET", "/famille/admin/inscriptions/qr.png", "", nil, e.cookies["famille"])
	if q.Code != 200 || q.Header().Get("Content-Type") != "image/png" || q.Body.String() != "PNG:"+testOrigin+"/famille/rejoindre/tok1" {
		t.Errorf("QR : %d %q %q", q.Code, q.Header().Get("Content-Type"), q.Body.String())
	}
	if q := e.do("GET", "/famille/admin/inscriptions/qr.png", "", nil, e.cookies["amis"]); q.Code != 303 {
		t.Errorf("QR d'un autre groupe : %d", q.Code)
	}
}

func TestRenewNeedsInlineConfirmation(t *testing.T) {
	e := newEnv(t)
	e.post(t, "famille", "/famille/admin/inscriptions/mode", url.Values{"enabled": {"1"}})
	page := e.do("GET", "/famille/admin/inscriptions", "fr", nil, e.cookies["famille"]).Body.String()
	if strings.Contains(page, `role="alertdialog"`) {
		t.Error("confirmation affichée d'emblée")
	}
	asked := e.do("GET", "/famille/admin/inscriptions?confirm=renew", "fr", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(asked, `role="alertdialog"`) || e.inv.callCount() != 1 {
		t.Error("confirmation intégrée absente ou GET modifiant")
	}
	w := e.post(t, "famille", "/famille/admin/inscriptions/renouveler", nil)
	if w.Code != 303 || e.inv.callCount() != 2 {
		t.Errorf("renouvellement : %d, %d appels", w.Code, e.inv.callCount())
	}
	if q := e.do("GET", "/famille/admin/inscriptions/qr.png", "", nil, e.cookies["famille"]); !strings.HasSuffix(q.Body.String(), "tok2") {
		t.Error("QR de l'ancien lien")
	}
}

func TestPendingActions(t *testing.T) {
	e := newEnv(t)
	for action, want := range map[string]string{"valider": "validate:7", "renvoyer": "resend:7", "supprimer": "delete:7"} {
		w := e.post(t, "famille", "/famille/admin/inscriptions/7/"+action, nil)
		if w.Code != 303 {
			t.Errorf("%s : %d", action, w.Code)
		}
		if got := e.ver.calls[len(e.ver.calls)-1]; got != want {
			t.Errorf("%s : appel %q", action, got)
		}
	}
	if w := e.post(t, "famille", "/famille/admin/inscriptions/abc/valider", nil); w.Code != 404 {
		t.Errorf("id invalide : %d", w.Code)
	}
}

func TestResendTooSoonShowsInlineError(t *testing.T) {
	e := newEnv(t)
	e.ver.resend = errTooSoon()
	w := e.post(t, "famille", "/famille/admin/inscriptions/7/renvoyer", nil)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), `role="alert"`) ||
		!strings.Contains(w.Body.String(), "Patiente une minute") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}

func TestLimitsAndValidation(t *testing.T) {
	e := newEnv(t)
	w := e.post(t, "famille", "/famille/admin/inscriptions/limites", url.Values{"capacity": {"12"}, "closes_on": {"2026-11-20"}})
	if w.Code != 303 || e.inv.cap[1] != 12 || e.inv.closes[1] == nil || e.inv.closes[1].Format("2006-01-02") != "2026-11-21" {
		t.Fatalf("limites : %d %v", w.Code, e.inv.closes[1])
	}
	if b := e.do("GET", "/famille/admin/inscriptions", "", nil, e.cookies["famille"]).Body.String(); !strings.Contains(b, `value="2026-11-20"`) {
		t.Error("date de clôture non réaffichée")
	}
	if w := e.post(t, "famille", "/famille/admin/inscriptions/limites", url.Values{"capacity": {"5000"}}); w.Code != 422 {
		t.Errorf("plafond trop haut : %d", w.Code)
	}
	if w := e.post(t, "famille", "/famille/admin/inscriptions/limites", url.Values{"capacity": {"x"}}); w.Code != 422 {
		t.Errorf("plafond illisible : %d", w.Code)
	}
}

func TestClosedAfterDraw(t *testing.T) {
	e := newEnv(t)
	e.draw.done = true
	b := e.do("GET", "/famille/admin/inscriptions", "fr", nil, e.cookies["famille"]).Body.String()
	if !strings.Contains(b, "Inscriptions par lien fermées") || !strings.Contains(b, "/famille/admin/tirage") {
		t.Error("état fermé après tirage absent")
	}
}
