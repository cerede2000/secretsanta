package targetpage

import (
	"testing"
	"time"
)

// Le compte à rebours est rendu par le serveur (jours, heures, minutes à
// l'heure de l'horloge injectée) et jamais une cible avant l'ouverture.
func TestWaitPageServerRenderedCountdown(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "compte-a-rebours", at(16*24*time.Hour+4*time.Hour+23*time.Minute+30*time.Second), true)
	e.login(t, s.gid, s.normal[0])
	body := page(e, s.slug)
	mustContain(t, body, `role="timer"`)
	mustContain(t, body, `data-unit="days">16<`)
	mustContain(t, body, `data-unit="hours">04<`)
	mustContain(t, body, `data-unit="minutes">23<`)
	mustContain(t, body, "16 jours, 4 heures, 23 minutes")
	mustContain(t, body, `data-until="2026-12-01T14:23:30Z"`)
	mustContain(t, body, `/static/js/draw/countdown.js`)
	mustNotContain(t, body, "draw-card")

	// À la date, la révélation est ouverte : la page de l'attente laisse la
	// place à la découverte.
	e.clock.Advance(17 * 24 * time.Hour)
	e.login(t, s.gid, s.normal[0])
	w := e.do("GET", "/"+s.slug+"/participant", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/"+s.slug+"/decouverte" {
		t.Fatalf("à la date : %d %q", w.Code, w.Header().Get("Location"))
	}
}

// Avant le tirage : pas de compte à rebours, un message d'attente.
func TestWaitPageBeforeDrawHasNoCountdown(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "avant-tirage", at(48*time.Hour), false)
	e.login(t, s.gid, s.normal[0])
	body := page(e, s.slug)
	mustContain(t, body, "Le tirage n'a pas encore eu lieu")
	mustNotContain(t, body, "data-countdown")
	mustNotContain(t, body, "countdown.js")
}
