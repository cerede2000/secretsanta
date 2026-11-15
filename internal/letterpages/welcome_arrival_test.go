package letterpages

import (
	"net/http"
	"strings"
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
)

func TestWelcomeShowsBannerAndFocusOnIntro(t *testing.T) {
	e := newEnv(t)
	a := e.add(t, "Alice", participants.RoleNormal)
	c := e.login(t, a)
	rec := e.get(t, "/noel/lettre?bienvenue=1", c)
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="bienvenue"`) || !strings.Contains(body, "Bienvenue") {
		t.Error("bandeau d'accueil absent")
	}
	ta := body[strings.Index(body, `<textarea id="intro"`):]
	ta = ta[:strings.Index(ta, ">")]
	if !strings.Contains(ta, " autofocus") {
		t.Errorf("focus absent sur l'introduction : %s", ta)
	}
	plain := e.get(t, "/noel/lettre", c).Body.String()
	if strings.Contains(plain, `id="bienvenue"`) || strings.Contains(plain, "autofocus") {
		t.Error("bandeau ou focus sans bienvenue=1")
	}
}

func TestWelcomeChangesNoAccessRight(t *testing.T) {
	e := newEnv(t)
	alice := e.add(t, "Alice", participants.RoleNormal)
	bob := e.add(t, "Bobby", participants.RoleNormal)
	e.add(t, "Carla", participants.RoleNormal)
	if _, err := e.letters.SetIntro(ctx, e.gid, bob, "Secret de Bobby"); err != nil {
		t.Fatal(err)
	}
	// Un étranger (sans session) n'obtient aucune lettre, même avec bienvenue=1.
	for _, p := range []string{"/noel/lettre?bienvenue=1", "/noel/lettre-recue/1?bienvenue=1", "/noel/lettre/image/1?bienvenue=1"} {
		rec := e.get(t, p, nil)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "Secret de Bobby") {
			t.Errorf("%s : accès étranger %d", p, rec.Code)
		}
	}
	// La page d'écriture ne montre que la lettre de la personne connectée.
	body := e.get(t, "/noel/lettre?bienvenue=1", e.login(t, alice)).Body.String()
	if strings.Contains(body, "Secret de Bobby") {
		t.Error("la lettre d'un autre apparaît")
	}
	// Un tireur ne lit pas avant la révélation, ni une cible non découverte.
	e.mustDraw(t)
	ps := e.pairs(t)
	var bobPair rawPair
	for _, p := range ps {
		if p.Receiver == bob {
			bobPair = p
		}
	}
	giver := e.login(t, bobPair.Giver)
	if rec := e.get(t, "/noel/lettre-recue/"+itoa(bobPair.ID)+"?bienvenue=1", giver); rec.Code != http.StatusNotFound {
		t.Errorf("lecture avant révélation : %d", rec.Code)
	}
	// Un tiers qui n'est pas le tireur de Bobby ne lit pas la lettre de Bobby.
	e.revealed2(t)
	for _, p := range ps {
		if p.Giver == bobPair.Giver {
			continue
		}
		rec := e.get(t, "/noel/lettre-recue/"+itoa(bobPair.ID)+"?bienvenue=1", e.login(t, p.Giver))
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Secret de Bobby") {
			t.Errorf("tireur %d lit la lettre d'une autre cible : %d", p.Giver, rec.Code)
		}
	}
	// Le vrai tireur, après découverte, la lit.
	rec := e.get(t, "/noel/lettre-recue/"+itoa(bobPair.ID), e.login(t, bobPair.Giver))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Secret de Bobby") {
		t.Errorf("le vrai tireur ne lit pas : %d", rec.Code)
	}
	_ = letter.MaxIdeas
}

// revealed2 ouvre la révélation et découvre toutes les cibles (tirage déjà fait).
func (e *env) revealed2(t *testing.T) {
	t.Helper()
	e.clock.Set(e.revealA)
	for _, p := range e.pairs(t) {
		if _, err := e.draw.MarkDiscovered(ctx, participants.ParticipantActor(e.gid, p.Giver), e.gid, p.ID); err != nil {
			t.Fatalf("découverte : %v", err)
		}
	}
}
