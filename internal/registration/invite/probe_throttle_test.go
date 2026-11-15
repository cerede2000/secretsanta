package invite

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/registration/code"
)

// F11.3 : les réponses « adresse déjà utilisée » et « nom déjà inscrit » ne
// sont révélées qu'à un détenteur du jeton d'invitation et sous les plafonds.
// Chaque soumission valide au format est donc comptée AVANT ces contrôles :
// sans cela, des soumissions qui n'aboutissent jamais à une création ne
// touchaient aucun plafond.

// throttleAdapter branche le limiteur réel de l'inscription (code.Service)
// sur le port Throttle.
type throttleAdapter struct{ svc *code.Service }

func (a throttleAdapter) AllowSubmission(_ context.Context, g int64, ip string) error {
	return a.svc.AllowSubmission(g, ip)
}

func (a throttleAdapter) AllowRegistration(_ context.Context, g int64, ip, _ string) error {
	return a.svc.AllowRegistration(g, ip)
}

func (e *env) throttle(t *testing.T) {
	t.Helper()
	cs, err := code.New(code.Options{
		Store: code.NewMemoryStore(), Clock: e.clock,
		Mailer: code.MailerFunc(func(context.Context, code.Mail) error { return nil }),
		Link:   func(slug, tok string) string { return slug + tok },
		Key:    []byte("cle-de-signature-de-test-0123456789"),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc.cfg.Throttle = throttleAdapter{cs}
}

func (e *env) submitErr(slug, tok string, s Submission) (*Result, error) {
	return e.svc.Submit(context.Background(), slug, tok, s)
}

func TestNameProbingIsCappedPerIP(t *testing.T) {
	e := newEnv(t)
	e.throttle(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "paul@example.org")

	limit := code.SubmissionsPerIP.Limit
	for i := 0; i < limit; i++ {
		r, err := e.submitErr("noel", l.Token, sub("Paul", "Durand", fmt.Sprintf("sonde%d@example.org", i)))
		if err != nil || r.Outcome != OutcomeNameTaken {
			t.Fatalf("sonde %d : %v %+v", i+1, err, r)
		}
	}
	if _, err := e.submitErr("noel", l.Token, sub("Paul", "Durand", "autre@example.org")); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("sonde au-delà du plafond : %v", err)
	}
	// Une autre adresse IP, ou un autre groupe, n'est pas touché.
	other := sub("Paul", "Durand", "x@example.org")
	other.IP = "198.51.100.4"
	if r, err := e.submitErr("noel", l.Token, other); err != nil || r.Outcome != OutcomeNameTaken {
		t.Fatalf("autre IP : %v %+v", err, r)
	}
	// La fenêtre de 15 minutes rouvre.
	e.clock.Advance(code.SubmissionsPerIP.Window)
	if r, err := e.submitErr("noel", l.Token, sub("Paul", "Durand", "z@example.org")); err != nil || r.Outcome != OutcomeNameTaken {
		t.Fatalf("après la fenêtre : %v %+v", err, r)
	}
}

func TestAddressProbingIsCappedPerIP(t *testing.T) {
	e := newEnv(t)
	e.throttle(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "foyer@example.org")

	for i := 0; i < code.SubmissionsPerIP.Limit; i++ {
		r, err := e.submitErr("noel", l.Token, sub("Léa", fmt.Sprintf("Sonde%d", i), "foyer@example.org"))
		if err != nil || r.Outcome != OutcomeConfirmShared {
			t.Fatalf("sonde %d : %v %+v", i+1, err, r)
		}
	}
	if _, err := e.submitErr("noel", l.Token, sub("Léa", "Derniere", "foyer@example.org")); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("sonde au-delà du plafond : %v", err)
	}
	if e.codes.count() != 0 {
		t.Fatal("un code est parti pendant le sondage")
	}
}

func TestProbingCapAnswers429OverHTTP(t *testing.T) {
	e := newEnv(t)
	e.throttle(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "paul@example.org")
	h := e.server(t)
	path := "/noel/rejoindre/" + l.Token

	last := 0
	for i := 0; i < code.SubmissionsPerIP.Limit+5; i++ {
		last = postForm(t, h, path, formOf("Paul", "Durand", fmt.Sprintf("s%d@example.org", i))).Code
		if i < code.SubmissionsPerIP.Limit && last == http.StatusTooManyRequests {
			t.Fatalf("429 trop tôt à la soumission %d", i+1)
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("après le plafond : %d, 429 attendu", last)
	}
}

// Une personne qui corrige une faute doit pouvoir renvoyer plusieurs fois :
// nom pris, adresse partagée à confirmer, puis création. Et le plafond de
// créations (5) reste celui de F11.3 : les sondes ne le consomment pas.
func TestHonestCorrectionsStayUnderBothCaps(t *testing.T) {
	e := newEnv(t)
	e.throttle(t)
	l := e.enable(t, e.g1)
	e.addParticipant(t, e.g1, "Paul", "Durand", "foyer@example.org")

	for _, s := range []Submission{
		sub("Paul", "Durand", "foyer@example.org"), // nom pris
		sub("Paul", "Durand", "foyer@example.org"), // même faute
		sub("Léa", "Durand", "foyer@example.org"),  // adresse partagée à confirmer
	} {
		if _, err := e.submitErr("noel", l.Token, s); err != nil {
			t.Fatalf("correction légitime refusée : %v", err)
		}
	}
	for i := 0; i < code.RegistrationsPerIP.Limit; i++ {
		s := sub("Léa", fmt.Sprintf("Nom%d", i), "foyer@example.org")
		s.ConfirmShared = true
		if r, err := e.submitErr("noel", l.Token, s); err != nil || r.Outcome != OutcomeCodeSent {
			t.Fatalf("création %d : %v %+v", i+1, err, r)
		}
	}
	s := sub("Léa", "Sixieme", "foyer@example.org")
	s.ConfirmShared = true
	if _, err := e.submitErr("noel", l.Token, s); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("6e création : %v", err)
	}
}

// Les saisies invalides au format ne révèlent rien : elles ne sont pas
// comptées (une faute de frappe ne coûte rien).
func TestInvalidSubmissionsAreNotCounted(t *testing.T) {
	e := newEnv(t)
	e.throttle(t)
	l := e.enable(t, e.g1)
	for i := 0; i < 3*code.SubmissionsPerIP.Limit; i++ {
		r, err := e.submitErr("noel", l.Token, sub("", "", "pas-une-adresse"))
		if err != nil || r.Outcome != OutcomeInvalid {
			t.Fatalf("saisie invalide %d : %v %+v", i+1, err, r)
		}
	}
	if r, err := e.submitErr("noel", l.Token, sub("Léa", "Martin", "lea@example.org")); err != nil || r.Outcome != OutcomeCodeSent {
		t.Fatalf("saisie valide ensuite : %v %+v", err, r)
	}
}
