package participants

import (
	"reflect"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestLocaleOptionalAndValidated(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	if p.Locale != "" {
		t.Fatal("locale facultative")
	}
	for _, ok := range []string{"fr", "en", "pt-BR", "zh-Hant-TW", "de"} {
		if err := e.svc.SetLocale(t.Context(), e.admin(), e.g1, p.ID, ok); err != nil {
			t.Fatalf("%q : %v", ok, err)
		}
		got, _ := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
		if got.Locale != ok {
			t.Fatalf("relu %q, attendu %q", got.Locale, ok)
		}
	}
	for _, bad := range []string{"f", "français", "fr_FR", "fr-", "123", "fr FR", "<script>", "a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q"} {
		wantKind(t, e.svc.SetLocale(t.Context(), e.admin(), e.g1, p.ID, bad), errs.KindValidation)
	}
	// Vider = langue par défaut.
	if err := e.svc.SetLocale(t.Context(), e.admin(), e.g1, p.ID, ""); err != nil {
		t.Fatal(err)
	}
	// À la création.
	q, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "C", Name: "D", Locale: "en"})
	if err != nil || q.Locale != "en" {
		t.Fatalf("%+v %v", q, err)
	}
	_, err = e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "E", Name: "F", Locale: "xx_"})
	wantKind(t, err, errs.KindValidation)
}

func TestLocaleEditableByPersonAndNotShared(t *testing.T) {
	e := newEnv(t)
	p := e.add(t, "A", "B")
	q := e.add(t, "C", "D")
	if err := e.svc.SetLocale(t.Context(), ParticipantActor(e.g1, p.ID), e.g1, p.ID, "en"); err != nil {
		t.Fatal(err)
	}
	wantKind(t, e.svc.SetLocale(t.Context(), ParticipantActor(e.g1, p.ID), e.g1, q.ID, "en"), errs.KindForbidden)
	wantKind(t, e.svc.SetLocale(t.Context(), Actor{}, e.g1, p.ID, "en"), errs.KindUnauthorized)
	wantKind(t, e.svc.SetLocale(t.Context(), ParticipantActor(e.g2, p.ID), e.g1, p.ID, "en"), errs.KindForbidden)

	// Ni vue publique ni liste d'un participant.
	for i := 0; i < reflect.TypeOf(PublicView{}).NumField(); i++ {
		if n := reflect.TypeOf(PublicView{}).Field(i).Name; n == "Locale" || n == "Email" {
			t.Errorf("PublicView expose %s", n)
		}
	}
	got, _ := e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
	if got.Public() != (PublicView{ID: p.ID, FirstName: "A", Name: "B"}) {
		t.Fatalf("%+v", got.Public())
	}
	l, _ := e.svc.List(t.Context(), ParticipantActor(e.g1, q.ID), e.g1)
	for _, x := range l {
		if x.Locale != "" {
			t.Fatal("locale exposée à un participant")
		}
	}
	// L'édition après tirage ne touche pas à la langue.
	if _, err := e.svc.Update(t.Context(), e.admin(), e.g1, p.ID, Input{FirstName: "A", Name: "B2"}); err != nil {
		t.Fatal(err)
	}
	got, _ = e.svc.Get(t.Context(), e.admin(), e.g1, p.ID)
	if got.Locale != "en" {
		t.Fatal("langue perdue")
	}
}
