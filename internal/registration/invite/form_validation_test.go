package invite

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// F11.AC5 : prénom, nom et adresse valide obligatoires ; saisies nettoyées ;
// longueurs bornées ; champ piège rempli refusé sans révéler le motif.

func TestRequiredFields(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	cases := []struct {
		sub   Submission
		field string
		code  string
	}{
		{sub("", "Lebrun", "anne@example.org"), FieldFirstName, "registration.first_name_required"},
		{sub("  \t ", "Lebrun", "anne@example.org"), FieldFirstName, "registration.first_name_required"},
		{sub("Anne", "", "anne@example.org"), FieldName, "registration.name_required"},
		{sub("Anne", "​\u0007", "anne@example.org"), FieldName, "registration.name_required"},
		{sub("Anne", "Lebrun", ""), FieldEmail, "registration.email_required"},
	}
	for _, c := range cases {
		r := e.submit(t, "noel", l.Token, c.sub)
		if r.Outcome != OutcomeInvalid || r.Fields[c.field].Code != c.code || r.Fields[c.field].Message == "" {
			t.Errorf("%+v : %+v", c.sub, r)
		}
	}
	r := e.submit(t, "noel", l.Token, sub("", "", ""))
	if len(r.Fields) != 3 {
		t.Fatalf("trois champs en erreur attendus : %+v", r.Fields)
	}
	if e.pendingCount(t, e.g1) != 0 || e.codes.count() != 0 {
		t.Fatal("inscription créée malgré des champs manquants")
	}
}

func TestInvalidEmails(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	for _, m := range []string{"anne", "anne@", "@example.org", "anne@example", "anne@.org", "anne@example.",
		"Anne <anne@example.org>", "anne@exa mple.org", "anne@@example.org", "a,b@example.org"} {
		r := e.submit(t, "noel", l.Token, sub("Anne", "Lebrun", m))
		if r.Outcome != OutcomeInvalid || r.Fields[FieldEmail].Code != "registration.email_invalid" {
			t.Errorf("%q accepté : %+v", m, r)
		}
	}
	if e.pendingCount(t, e.g1) != 0 {
		t.Fatal("inscription créée avec une adresse invalide")
	}
}

func TestInputsAreCleanedAndBounded(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	r := e.submit(t, "noel", l.Token, sub("  Anne​ \t Marie\u0000 ", " Le\u0007brun  ", "  anne@example.org "))
	if r.Outcome != OutcomeCodeSent {
		t.Fatalf("saisie nettoyable refusée : %+v", r)
	}
	ps, _ := e.store.ListPending(t.Context(), e.g1, e.clock.Now())
	if len(ps) != 1 || ps[0].FirstName != "Anne Marie" || ps[0].Name != "Lebrun" || ps[0].Email != "anne@example.org" {
		t.Fatalf("saisie non nettoyée : %+v", ps)
	}
	if e.codes.issued[0].Email != "anne@example.org" {
		t.Fatalf("code envoyé à %q", e.codes.issued[0].Email)
	}

	long := strings.Repeat("é", NameMaxLen+1)
	ok := strings.Repeat("é", NameMaxLen)
	if r := e.submit(t, "noel", l.Token, sub(long, "Lebrun", "b@example.org")); r.Fields[FieldFirstName].Code != "registration.first_name_too_long" {
		t.Fatalf("prénom trop long : %+v", r)
	}
	if r := e.submit(t, "noel", l.Token, sub("Anne", long, "b@example.org")); r.Fields[FieldName].Code != "registration.name_too_long" {
		t.Fatalf("nom trop long : %+v", r)
	}
	email := strings.Repeat("a", EmailMaxLen-len("@example.org")+1) + "@example.org"
	if r := e.submit(t, "noel", l.Token, sub("Anne", "Lebrun2", email)); r.Fields[FieldEmail].Code != "registration.email_too_long" {
		t.Fatalf("adresse trop longue : %+v", r)
	}
	if r := e.submit(t, "noel", l.Token, sub(ok, ok, "c@example.org")); r.Outcome != OutcomeCodeSent {
		t.Fatalf("longueur maximale refusée : %+v", r)
	}
	if e.pendingCount(t, e.g1) != 2 {
		t.Fatalf("inscriptions : %d", e.pendingCount(t, e.g1))
	}
}

func TestHoneypotRefusedWithoutReason(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	real := e.submit(t, "noel", l.Token, sub("Anne", "Lebrun", "anne@example.org"))
	trapped := sub("Jean", "Petit", "jean@example.org")
	trapped.Honeypot = "http://spam.example"
	bot := e.submit(t, "noel", l.Token, trapped)
	// Même champ piège avec des champs invalides : même réponse, rien n'est dit.
	bad := sub("", "", "x")
	bad.Honeypot = "1"
	bot2 := e.submit(t, "noel", l.Token, bad)

	for _, r := range []*Result{bot, bot2} {
		if r.Ref == "" || r.Ref == real.Ref {
			t.Fatalf("référence : %+v", r)
		}
		a, b := *real, *r
		a.Ref, b.Ref = "", ""
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("réponse distincte pour le champ piège :\n%+v\n%+v", a, b)
		}
	}
	if e.pendingCount(t, e.g1) != 1 || e.codes.count() != 1 {
		t.Fatal("le champ piège a créé une inscription ou envoyé un code")
	}

	// Par HTTP : même statut et même page qu'une inscription acceptée.
	h := e.server(t)
	ok := postForm(t, h, l.Path, formOf("Lou", "Roux", "lou@example.org"))
	f := formOf("Max", "Roux", "max@example.org")
	f.Set(FieldHoneypot, "spam")
	trap := postForm(t, h, l.Path, f)
	strip := func(b string) string { return csrfRe.ReplaceAllString(b, "") } // jeton CSRF masqué, propre à chaque requête
	if ok.Code != http.StatusOK || trap.Code != ok.Code || strip(trap.Body.String()) != strip(ok.Body.String()) {
		t.Fatalf("pages différentes : %d %d\n%s\n%s", ok.Code, trap.Code, ok.Body.String(), trap.Body.String())
	}
	if e.pendingCount(t, e.g1) != 2 || e.codes.count() != 2 {
		t.Fatal("le champ piège a créé une inscription (HTTP)")
	}
}

func TestFieldErrorsRenderedInPage(t *testing.T) {
	e := newEnv(t)
	l := e.enable(t, e.g1)
	rec := postForm(t, e.server(t), l.Path, formOf("", "Lebrun", "pas-une-adresse"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("statut : %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-field="prenom"`) || !strings.Contains(body, `data-field="email"`) ||
		!strings.Contains(body, "<form") {
		t.Fatalf("erreurs non intégrées : %s", body)
	}
}
