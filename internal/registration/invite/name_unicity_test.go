package invite

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F11.AC6 : un prénom et nom déjà présents (participants via NameExists ou
// inscriptions en attente), quelles que soient la casse et les accents,
// répondent seulement « ce nom est déjà inscrit » sans nommer personne et
// proposent le lien magique ou une précision.

func assertNameTaken(t *testing.T, r *Result, slug string, secrets ...string) {
	t.Helper()
	if r.Outcome != OutcomeNameTaken || r.Problem.Message != "Ce nom est déjà inscrit." ||
		r.Problem.Code != "registration.name_taken" {
		t.Fatalf("doublon non signalé : %+v", r)
	}
	var link, precision bool
	for _, o := range r.Offers {
		switch o.Kind {
		case OfferMagicLink:
			link = o.Path == "/"+slug+"/connexion"
		case OfferPrecision:
			precision = strings.Contains(o.Message, "Camille M.")
		}
	}
	if !link || !precision {
		t.Fatalf("propositions : %+v", r.Offers)
	}
	shown := *r
	shown.Values = Values{} // la saisie de la personne elle-même
	dump := fmt.Sprintf("%+v", shown)
	for _, s := range secrets {
		if strings.Contains(strings.ToLower(dump), strings.ToLower(s)) {
			t.Fatalf("la réponse cite %q : %s", s, dump)
		}
	}
}

func TestNameTakenByParticipant(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Camille", "Martin", "camille@example.org")
	for _, v := range [][2]string{{"camille", "martin"}, {"CAMILLE", "MARTÍN"}, {"Camïlle", "  Martin "}} {
		r := e.submit(t, "noel", l.Token, sub(v[0], v[1], "autre@example.org"))
		assertNameTaken(t, r, "noel", "camille@example.org")
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("doublon enregistré")
	}
	// Une précision suffit.
	if r := e.submit(t, "noel", l.Token, sub("Camille", "M.", "autre@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("précision refusée : %+v", r)
	}
	// Le même nom est libre dans un autre groupe.
	l2 := e.enable(t, e.g2)
	if r := e.submit(t, "famille", l2.Token, sub("Camille", "Martin", "c@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("unicité débordant du groupe : %+v", r)
	}
}

func TestNameTakenByPendingRegistration(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	if r := e.submit(t, "noel", l.Token, sub("Élodie", "Durand", "elodie@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatal(r)
	}
	r := e.submit(t, "noel", l.Token, sub("elodie", "DURAND", "intrus@example.org"))
	assertNameTaken(t, r, "noel", "elodie@example.org", "Élodie")
	if e.pendingCount(t, e.g1) != 1 || e.codes.count() != 1 {
		t.Fatal("doublon en attente enregistré")
	}
	// Une inscription expirée ne bloque plus le nom.
	e.clock.Advance(PendingTTL)
	if r := e.submit(t, "noel", l.Token, sub("elodie", "durand", "elodie@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("nom d'une inscription expirée bloqué : %+v", r)
	}
}

func TestNameTakenPage(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Camille", "Martin", "camille@example.org")
	rec := postForm(t, e.server(t), l.Path, formOf("camille", "martin", "x@example.org"))
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "Ce nom est déjà inscrit.") ||
		!strings.Contains(body, `href="/noel/connexion"`) || strings.Contains(body, "camille@example.org") ||
		strings.Contains(body, "Martin") {
		t.Fatalf("page : %d %s", rec.Code, body)
	}
}

// La règle d'unicité n'est pas copiée : le paquet passe par
// participants.NormalizeName et participants.NameExists.
func TestNameRuleNotDuplicated(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var usesNameExists, usesNormalize bool
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, banned := range []string{"name_key", "norm.NFD", "unicode.Mn", "runes.Remove", "strings.EqualFold"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s copie la règle de nom (%s)", f, banned)
			}
		}
		usesNameExists = usesNameExists || strings.Contains(src, "participants.NameExists(")
		usesNormalize = usesNormalize || strings.Contains(src, "participants.NormalizeName(")
	}
	if !usesNameExists || !usesNormalize {
		t.Fatal("participants.NameExists et participants.NormalizeName doivent être utilisés")
	}
}
