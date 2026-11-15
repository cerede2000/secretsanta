package groups

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F2.AC3 : les données de GET /<slug> ne contiennent jamais de nom, prénom
// ni compte de participants (ni budget, ni consigne, ni secret).

func TestPublicViewHasOnlySlugAndName(t *testing.T) {
	typ := reflect.TypeOf(PublicView{})
	var fields []string
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name)
	}
	sort.Strings(fields)
	if strings.Join(fields, ",") != "Name,Slug" {
		t.Fatalf("PublicView expose %v : seuls Name et Slug sont autorisés", fields)
	}
}

func TestPublicPageReturnsOnlyGroupName(t *testing.T) {
	e := newEnv(t)
	in := validInput("famille")
	in.Budget = "30"
	in.GiftInstructions = "Un cadeau fait main"
	r := mustCreate(t, e, in)

	v, err := e.svc.PublicPage(context.Background(), "famille")
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "Famille Martin" || v.Slug != "famille" {
		t.Fatalf("%+v", v)
	}
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if len(m) != 2 {
		t.Fatalf("réponse publique : %s", b)
	}
	low := strings.ToLower(string(b))
	for _, leak := range []string{"participant", "count", "nombre", "prenom", "prénom", "budget", "30", "fait main", r.AdminSecret} {
		if strings.Contains(low, strings.ToLower(leak)) {
			t.Fatalf("la réponse publique divulgue %q : %s", leak, b)
		}
	}
}

func TestPublicPageUnknownAndInvalidSlugIdentical(t *testing.T) {
	e := newEnv(t)
	mustCreate(t, e, validInput("famille"))
	var msgs []string
	for _, s := range []string{"inconnu", "Famille", "../famille", ""} {
		v, err := e.svc.PublicPage(context.Background(), s)
		wantFrench(t, err, errs.KindNotFound)
		if v != (PublicView{}) {
			t.Fatalf("données renvoyées pour %q : %+v", s, v)
		}
		msgs = append(msgs, errs.UserMessage(err))
	}
	for _, m := range msgs {
		if m != msgs[0] {
			t.Fatalf("réponses différentes : %v", msgs)
		}
	}
}
