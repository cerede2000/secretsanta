package public

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/registration/invite"
)

func TestFormGetRendersFieldsAndCSRF(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, http.MethodGet, joinPath, nil, "fr")
	if r.Code != 200 {
		t.Fatalf("%d", r.Code)
	}
	b := r.Body.String()
	for _, w := range []string{`name="prenom"`, `name="nom"`, `name="email"`, `name="csrf_token"`, `name="site_web"`, "Rejoins le Secret Santa"} {
		if !strings.Contains(b, w) {
			t.Errorf("%q absent", w)
		}
	}
	if r.Header().Get("Referrer-Policy") != "no-referrer" || r.Header().Get("Cache-Control") != "no-store" {
		t.Error("en-têtes du jeton d'invitation absents")
	}
}

func TestWrongTokenGivesNeutralResponse(t *testing.T) {
	e := newEnv(t)
	a := e.do(t, http.MethodGet, "/noel/rejoindre/mauvais-jeton", nil, "fr")
	b := e.do(t, http.MethodGet, "/inconnu/rejoindre/"+joinToken, nil, "fr")
	if a.Code != 404 || b.Code != 404 || a.Body.String() != b.Body.String() {
		t.Fatalf("réponse neutre non uniforme : %d/%d", a.Code, b.Code)
	}
}

func TestFieldErrorsAreShownUnderTheField(t *testing.T) {
	e := newEnv(t)
	e.invites.result = &invite.Result{
		Outcome: invite.OutcomeInvalid,
		Fields: map[string]invite.Problem{
			invite.FieldName:  {Code: "registration.name_required"},
			invite.FieldEmail: {Code: "registration.email_invalid"},
		},
		Values: invite.Values{FirstName: "Camille", Email: "x"},
	}
	r := e.do(t, http.MethodPost, joinPath, url.Values{"prenom": {"Camille"}}, "fr")
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d", r.Code)
	}
	b := r.Body.String()
	for _, w := range []string{`aria-invalid="true" aria-describedby="reg-name-err"`, `id="reg-name-err" class="reg-msg reg-msg-error" role="alert"`,
		"Le nom est obligatoire.", "L&#39;adresse mail n&#39;est pas valide.", `value="Camille"`} {
		if !strings.Contains(b, w) {
			t.Errorf("%q absent", w)
		}
	}
	if strings.Contains(b, `aria-describedby="reg-first-err"`) {
		t.Error("erreur sur un champ valide")
	}
}

func TestNameTakenOffersLoginLinkAndSharedAddressAsksConfirmation(t *testing.T) {
	e := newEnv(t)
	e.invites.result = &invite.Result{Outcome: invite.OutcomeNameTaken,
		Fields: map[string]invite.Problem{invite.FieldName: {Code: "registration.name_taken"}}}
	b := e.do(t, http.MethodPost, joinPath, url.Values{"prenom": {"a"}}, "fr").Body.String()
	if !strings.Contains(b, `href="/noel/connexion"`) || !strings.Contains(b, "Ce nom est déjà inscrit.") {
		t.Error("offre « Recevoir mon lien » absente")
	}
	e.invites.result = &invite.Result{Outcome: invite.OutcomeConfirmShared, Values: invite.Values{FirstName: "A", Name: "B", Email: "a@b.fr"}}
	b = e.do(t, http.MethodPost, joinPath, url.Values{"prenom": {"a"}}, "fr").Body.String()
	if !strings.Contains(b, `name="adresse_partagee"`) {
		t.Error("case « adresse partagée » absente")
	}
}

func TestCodeStepAndRefusal(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, http.MethodPost, joinPath, url.Values{"prenom": {"a"}}, "fr")
	b := r.Body.String()
	if r.Code != 200 || !strings.Contains(b, `action="/noel/inscription/code"`) || !strings.Contains(b, `name="ref" value="ref-1"`) ||
		!strings.Contains(b, `name="retour" value="`+joinPath+`"`) {
		t.Fatalf("page du code incorrecte : %d", r.Code)
	}
	bad := e.do(t, http.MethodPost, "/noel/inscription/code", url.Values{"ref": {"ref-1"}, "code": {"ZZZZZZ"}, "retour": {joinPath}}, "fr")
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d", bad.Code)
	}
	bb := bad.Body.String()
	if !strings.Contains(bb, `role="alert"`) || !strings.Contains(bb, `aria-invalid="true"`) || !strings.Contains(bb, `href="`+joinPath+`"`) {
		t.Error("refus du code non intégré à l'interface")
	}
}

func TestConfirmLinkGetChangesNothing(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, http.MethodGet, "/noel/inscription/confirmer/lien", nil, "fr")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "ABC123") || e.verify.total() != 0 {
		t.Fatalf("GET : %d, vérifications %d", r.Code, e.verify.total())
	}
	if bad := e.do(t, http.MethodGet, "/noel/inscription/confirmer/autre", nil, "fr"); bad.Code != 404 {
		t.Fatalf("lien faux : %d", bad.Code)
	}
}

func TestClosedAfterDrawShowsMessage(t *testing.T) {
	e := newEnv(t)
	e.invites.closed = invite.ClosedDraw
	r := e.do(t, http.MethodGet, joinPath, nil, "fr")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Inscriptions closes") || strings.Contains(r.Body.String(), `name="prenom"`) {
		t.Fatal("fermeture mal rendue")
	}
}
