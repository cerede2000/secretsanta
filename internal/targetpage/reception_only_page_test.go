package targetpage

import (
	"testing"
	"time"
)

// Une personne en réception seule voit l'accueil dédié, avant le tirage,
// pendant l'attente et après l'ouverture : jamais de cible, de compte à
// rebours, de bouton de découverte ni de préférence ; toujours l'invitation à
// écrire sa lettre.
func TestReceptionOnlyPage(t *testing.T) {
	for _, c := range []struct {
		name     string
		revealAt *time.Time
		drawIt   bool
	}{
		{"avant le tirage", nil, false},
		{"attente de la révélation", at(72 * time.Hour), true},
		{"révélation ouverte", nil, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := newScene(t, e, "reception-seule", c.revealAt, c.drawIt)
			e.login(t, s.gid, s.reception[0])
			for _, path := range []string{"/participant", "/ma-cible"} {
				w := e.do("GET", "/"+s.slug+path, "", nil)
				if w.Code != 200 {
					t.Fatalf("%s : %d", path, w.Code)
				}
				body := w.Body.String()
				mustContain(t, body, "Cette année, tu reçois un cadeau et tu n'en offres pas")
				mustContain(t, body, "Écrire ma lettre au Père Noël")
				mustContain(t, body, `href="/`+s.slug+`/lettre"`)
				for _, no := range []string{"data-countdown", "countdown.js", "role=\"timer\"", "decouverte", "draw-card", "Découvrir", switchMark, "Tu offres un cadeau à"} {
					mustNotContain(t, body, no)
				}
			}
		})
	}
}

// Le budget et le jour J n'apparaissent que s'ils sont définis.
func TestReceptionOnlyBudgetAndEventOnlyWhenDefined(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "reception-faits", nil, true)
	e.login(t, s.gid, s.reception[0])

	body := page(e, s.slug)
	mustNotContain(t, body, "budget par cadeau")
	mustNotContain(t, body, "remise des cadeaux")
	mustNotContain(t, body, "draw-infos")

	e.setBudget(t, s.gid, ptr(2500), 1)
	body = page(e, s.slug)
	mustContain(t, body, "25 €")
	mustContain(t, body, "budget par cadeau")
	mustNotContain(t, body, "remise des cadeaux")

	e.setEventDate(t, s.gid, "2026-12-24")
	body = page(e, s.slug)
	mustContain(t, body, "24 décembre")
	mustContain(t, body, "remise des cadeaux")

	e.setBudget(t, s.gid, nil, 2)
	body = page(e, s.slug)
	mustNotContain(t, body, "budget par cadeau")
	mustContain(t, body, "remise des cadeaux")
}
