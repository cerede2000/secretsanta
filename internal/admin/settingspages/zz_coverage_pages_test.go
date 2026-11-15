package settingspages

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminders"
	"secretsanta/internal/reveal"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

type zzReveal struct{ err error }

func (z zzReveal) Facts(context.Context, participants.Actor, int64) (*reveal.Facts, error) {
	return nil, z.err
}
func (z zzReveal) SetRevealDate(context.Context, participants.Actor, int64, *time.Time) error {
	return z.err
}

type zzRem struct{ err error }

func (z zzRem) Remind(context.Context, int64, reminders.Kind, []int64) (reminders.ManualResult, error) {
	return reminders.ManualResult{}, z.err
}

type zzPeople struct{ err error }

func (z zzPeople) List(context.Context, participants.Actor, int64) ([]participants.Participant, error) {
	return nil, z.err
}

type zzMail struct {
	st  queue.Status
	err error
}

func (z zzMail) SendTest(context.Context, queue.Item) (queue.Status, error) { return z.st, z.err }

// rebuild remplace le gestionnaire de l'environnement par des pages dont les
// dépendances ont été modifiées.
func (e *env) rebuild(t *testing.T, mod func(*Options)) {
	t.Helper()
	o := e.pages.o
	mod(&o)
	pages, err := NewPages(o)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	pages.Register(mux)
	pol, err := websec.NewPolicy(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	chain := websec.NewCSRF(pol, websec.CSRFOptions{}).Middleware(e.sessions.Middleware(mux))
	e.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chain.ServeHTTP(w, r.WithContext(negotiate.WithLang(r.Context(), "fr")))
	})
}

func TestZZRhythm(t *testing.T) {
	e := newEnv(t)
	gid := e.group("rythme")
	ck := e.adminCookie(gid)
	page := "/rythme/admin/reglages"
	act := page + "/relances"
	form := func(first, spacing, cap string) url.Values {
		return url.Values{"enabled": {"1"}, "first": {first}, "spacing": {spacing}, "cap": {cap}}
	}
	for name, c := range map[string]struct {
		f    url.Values
		code int
	}{
		"ok":          {form("3", "4", "2"), http.StatusSeeOther},
		"delai":       {form("0", "4", "2"), http.StatusUnprocessableEntity},
		"delai-texte": {form("x", "4", "2"), http.StatusUnprocessableEntity},
		"espacement":  {form("3", "99", "2"), http.StatusUnprocessableEntity},
		"plafond":     {form("3", "4", "9"), http.StatusUnprocessableEntity},
	} {
		if r := e.post(act, page, ck, c.f); r.Code != c.code {
			t.Errorf("%s : %d, %d attendu", name, r.Code, c.code)
		}
	}
}

func TestZZMailTest(t *testing.T) {
	e := newEnv(t)
	gid := e.group("courrier")
	ck := e.adminCookie(gid)
	page := "/courrier/admin/reglages"
	act := page + "/courrier/test"
	send := func(to string) int { return e.post(act, page, ck, url.Values{"test_to": {to}}).Code }

	for _, bad := range []string{"", "sans-arobase", "@exemple.fr", "a@", "a b@exemple.fr"} {
		if c := send(bad); c != http.StatusUnprocessableEntity {
			t.Errorf("adresse %q : %d", bad, c)
		}
	}
	if c := send("moi@exemple.fr"); c != http.StatusOK {
		t.Errorf("envoi : %d", c)
	}
	e.rebuild(t, func(o *Options) { o.Mail = zzMail{st: queue.Status{State: queue.StateFailed, LastError: "refusé"}} })
	if c := send("moi@exemple.fr"); c != http.StatusOK {
		t.Errorf("échec d'envoi : %d", c)
	}
	e.rebuild(t, func(o *Options) { o.Mail = zzMail{err: errs.Forbidden("non")} })
	if c := send("moi@exemple.fr"); c != http.StatusForbidden {
		t.Errorf("erreur du service : %d", c)
	}
	e.rebuild(t, func(o *Options) { o.SMTP = config.SMTP{} })
	if c := send("moi@exemple.fr"); c != http.StatusUnprocessableEntity {
		t.Errorf("SMTP éteint : %d", c)
	}
}

func TestZZRemind(t *testing.T) {
	e := newEnv(t)
	gid := e.group("relance")
	ids := e.addN(gid, "Ana", 3)
	ck := e.adminCookie(gid)
	page := "/relance/admin/relances"
	person := itoa(ids[0])

	if r := e.get(page+"?type=lettre", "fr", ck); r.Code != 200 {
		t.Errorf("page : %d", r.Code)
	}
	for name, c := range map[string]struct {
		f    url.Values
		code int
	}{
		"type-inconnu": {url.Values{"type": {"x"}, "person": {person}}, http.StatusUnprocessableEntity},
		"personne":     {url.Values{"type": {"lien"}}, http.StatusUnprocessableEntity},
		"ok":           {url.Values{"type": {"lecture"}, "person": {person, person, "abc"}}, http.StatusOK},
	} {
		if r := e.post(page, page, ck, c.f); r.Code != c.code {
			t.Errorf("%s : %d, %d attendu", name, r.Code, c.code)
		}
	}
	e.rebuild(t, func(o *Options) { o.Reminders = zzRem{err: errors.New("panne")} })
	if r := e.post(page, page, ck, url.Values{"type": {"lien"}, "person": {person}}); r.Code != http.StatusInternalServerError {
		t.Errorf("panne du moteur : %d", r.Code)
	}
	e.rebuild(t, func(o *Options) { o.People = zzPeople{err: errs.Forbidden("non")} })
	if r := e.get(page, "fr", ck); r.Code != http.StatusForbidden {
		t.Errorf("liste refusée : %d", r.Code)
	}
	if r := e.post(page, page, ck, url.Values{"type": {"lien"}, "person": {person}}); r.Code != http.StatusForbidden {
		t.Errorf("POST liste refusée : %d", r.Code)
	}
}

func TestZZSettingsFailures(t *testing.T) {
	e := newEnv(t)
	gid := e.group("panne")
	ck := e.adminCookie(gid)
	e.rebuild(t, func(o *Options) { o.Reveal = zzReveal{err: errs.NotFound("x")} })
	if r := e.get("/panne/admin/reglages", "fr", ck); r.Code != http.StatusNotFound {
		t.Errorf("lecture en échec : %d", r.Code)
	}
	e.rebuild(t, func(o *Options) { o.Reveal = zzReveal{err: errors.New("panne")} })
	if r := e.get("/panne/admin/reglages", "fr", ck); r.Code != http.StatusInternalServerError {
		t.Errorf("panne : %d", r.Code)
	}
	if r := e.get("/panne/admin/reglages", "fr", nil); r.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", r.Code)
	}
}
