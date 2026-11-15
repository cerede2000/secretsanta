package targetpage

import (
	"net/url"
	"testing"
	"time"
)

const (
	switchMark = `role="switch"`
	switchOff  = `aria-checked="false"`
	switchOn   = `aria-checked="true"`
)

func page(e *env, slug string) string {
	return e.do("GET", "/"+slug+"/participant", "", nil).Body.String()
}

// Avant le tirage : case non cochée par défaut, la personne bascule entre
// normal et un_seul, dans les deux sens.
func TestPreferenceTogglesBeforeDraw(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "pref-avant", nil, false)
	pid := s.normal[0]
	e.login(t, s.gid, pid)

	body := page(e, s.slug)
	mustContain(t, body, switchMark)
	mustContain(t, body, switchOff)
	mustNotContain(t, body, switchOn)
	mustNotContain(t, body, "disabled")
	mustNotContain(t, body, "Verrouillé")

	if w := e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {"1"}}); w.Code != 303 {
		t.Fatalf("passage à un seul cadeau : %d", w.Code)
	}
	if r := e.role(t, s.gid, pid); r != "un_seul" {
		t.Fatalf("rôle %q, un_seul attendu", r)
	}
	body = page(e, s.slug)
	mustContain(t, body, switchOn)

	if w := e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {"0"}}); w.Code != 303 {
		t.Fatalf("retour à normal : %d", w.Code)
	}
	if r := e.role(t, s.gid, pid); r != "normal" {
		t.Fatalf("rôle %q, normal attendu", r)
	}
	mustContain(t, page(e, s.slug), switchOff)
}

// Après le tirage : la préférence est verrouillée avec son explication, et un
// envoi forcé est refusé sans rien changer.
func TestPreferenceLockedAfterDraw(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "pref-apres", at(72*time.Hour), true)
	pid := s.normal[0]
	e.login(t, s.gid, pid)

	body := page(e, s.slug)
	mustContain(t, body, switchMark)
	mustContain(t, body, `aria-disabled="true"`)
	mustContain(t, body, "Verrouillé après le tirage")
	mustContain(t, body, switchOff)

	w := e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {"1"}})
	if w.Code != 409 {
		t.Fatalf("envoi forcé après le tirage : %d, 409 attendu", w.Code)
	}
	mustContain(t, w.Body.String(), "seul l'organisateur peut modifier")
	if r := e.role(t, s.gid, pid); r != "normal" {
		t.Fatalf("le rôle a changé après le tirage : %q", r)
	}
}

// Un_seul verrouillé après le tirage reste affiché coché.
func TestPreferenceLockedKeepsCheckedState(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "pref-verrou-coche", at(72*time.Hour), false)
	pid := s.normal[0]
	e.login(t, s.gid, pid)
	if w := e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {"1"}}); w.Code != 303 {
		t.Fatalf("%d", w.Code)
	}
	e.mustDraw(t, s.gid)
	body := page(e, s.slug)
	mustContain(t, body, switchOn)
	mustContain(t, body, "Verrouillé après le tirage")
}

// Jamais proposée à reception_seule ni offre_plus, et jamais d'accès à ces
// deux rôles par la page : un envoi forcé est refusé, le rôle ne change pas.
func TestPreferenceNeverOfferedToReceptionOrOffrePlus(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "pref-roles", nil, false)
	cases := []struct {
		name string
		id   int64
		role string
	}{
		{"reception_seule", s.reception[0], "reception_seule"},
		{"offre_plus", s.more[0], "offre_plus"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.login(t, s.gid, c.id)
			mustNotContain(t, page(e, s.slug), switchMark)
			for _, v := range []string{"1", "0", "normal", "reception_seule", "offre_plus"} {
				w := e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {v}, "role": {v}})
				if w.Code != 403 {
					t.Errorf("envoi forcé %q : %d, 403 attendu", v, w.Code)
				}
				mustNotContain(t, w.Body.String(), switchMark)
				if r := e.role(t, s.gid, c.id); r != c.role {
					t.Fatalf("le rôle est devenu %q", r)
				}
			}
		})
	}
}

// Les valeurs étrangères du formulaire ne donnent jamais un autre rôle que
// normal ou un_seul.
func TestPreferenceIgnoresRoleFieldInjection(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "pref-injection", nil, false)
	pid := s.normal[0]
	e.login(t, s.gid, pid)
	e.post(t, s.slug, "/preference", "", url.Values{"un_seul": {"reception_seule"}, "role": {"reception_seule"}})
	if r := e.role(t, s.gid, pid); r != "normal" {
		t.Fatalf("rôle %q", r)
	}
}
