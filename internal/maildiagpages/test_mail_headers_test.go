package maildiagpages_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/mail/queue"
)

const (
	pagePath = "/famille/admin/courrier/delivrabilite"
	testPath = pagePath + "/test"
)

// F6c.AC9 : le courriel de test est mis en file et la page affiche les
// en-têtes bruts du message, sans son corps.
func TestTestMailIsQueuedAndHeadersShown(t *testing.T) {
	e := newEnv(t)
	w := e.post(t, testPath, "", url.Values{"to": {"camille@exemple.fr"}})
	if w.Code != http.StatusOK {
		t.Fatalf("statut %d", w.Code)
	}
	if len(e.mailer.items) != 1 {
		t.Fatalf("%d courriel(s) mis en file", len(e.mailer.items))
	}
	it := e.mailer.items[0]
	if it.To != "camille@exemple.fr" || it.GroupID != e.gid || it.Type != "test" || !it.Essential || it.HTML == "" {
		t.Errorf("élément mis en file : %+v", it)
	}
	body := w.Body.String()
	for _, want := range []string{`id="maildiag-headers"`, "From: santa@exemple.fr", "Message-ID: &lt;abc@exemple.fr&gt;", "To: camille@exemple.fr", `role="status"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page sans %q", want)
		}
	}
	if strings.Contains(body, "Corps secret") {
		t.Error("le corps du message ne doit pas être affiché")
	}
}

func TestTestMailFailureAndInvalidAddress(t *testing.T) {
	e := newEnv(t)
	e.mailer.st = queue.Status{State: queue.StateFailed, LastError: "550 relais non autorisé"}
	w := e.post(t, testPath, "", url.Values{"to": {"camille@exemple.fr"}})
	if !strings.Contains(w.Body.String(), "550 relais non autorisé") || !strings.Contains(w.Body.String(), `role="alert"`) {
		t.Errorf("échec non affiché : %d", w.Code)
	}
	e.mailer.items = nil
	w = e.post(t, testPath, "", url.Values{"to": {"pas une adresse"}})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `id="maildiag-to-error"`) ||
		!strings.Contains(w.Body.String(), `aria-invalid="true"`) || len(e.mailer.items) != 0 {
		t.Errorf("adresse invalide : %d, %d envoi(s)", w.Code, len(e.mailer.items))
	}
	e.mailer.err = errBoom
	e.mailer.st = queue.Status{}
	w = e.post(t, testPath, "", url.Values{"to": {"camille@exemple.fr"}})
	if !strings.Contains(w.Body.String(), "boom") {
		t.Error("erreur du service non affichée")
	}
}
