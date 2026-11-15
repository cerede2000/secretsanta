package adjust

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// walkStrings parcourt une valeur et rend chaque chaîne, chaque entier et
// chaque type de champ rencontré.
func walkValue(t *testing.T, v reflect.Value, path string, onString func(string, string)) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkValue(t, v.Elem(), path, onString)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walkValue(t, v.Field(i), path+"."+v.Type().Field(i).Name, onString)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkValue(t, v.Index(i), fmt.Sprintf("%s[%d]", path, i), onString)
		}
	case reflect.Map:
		t.Fatalf("%s : une table n'a rien à faire dans l'aperçu", path)
	case reflect.String:
		onString(path, v.String())
	case reflect.Int64, reflect.Uint64:
		t.Fatalf("%s : un identifiant (%s) n'a rien à faire dans l'aperçu", path, v.Type())
	}
}

// noIdentityTypes vérifie qu'aucun champ de l'aperçu ne peut porter une
// personne : ni identifiant 64 bits, ni paire, ni contact.
func noIdentityTypes(t *testing.T, ty reflect.Type, path string, seen map[reflect.Type]bool) {
	t.Helper()
	if seen[ty] {
		return
	}
	seen[ty] = true
	switch ty.Name() {
	case "Pair", "CurrentPair", "Contact", "Person", "Notice":
		t.Fatalf("%s expose un type qui désigne une personne (%s)", path, ty)
	}
	switch ty.Kind() {
	case reflect.Int64, reflect.Uint64:
		t.Fatalf("%s est un identifiant possible (%s)", path, ty)
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		noIdentityTypes(t, ty.Elem(), path+"[]", seen)
	case reflect.Struct:
		for i := 0; i < ty.NumField(); i++ {
			noIdentityTypes(t, ty.Field(i).Type, path+"."+ty.Field(i).Name, seen)
		}
	}
}

// F4c.AC3 : l'aperçu (et le résultat) ne contient que des comptes et des
// textes sans aucun nom ; il dit combien de personnes sont touchées et si
// elles avaient déjà découvert leur cible.
func TestPreviewHasNoNames(t *testing.T) {
	noIdentityTypes(t, reflect.TypeOf(Preview{}), "Preview", map[reflect.Type]bool{})
	noIdentityTypes(t, reflect.TypeOf(Result{}), "Result", map[reflect.Type]bool{})

	e := newEnv(t)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	a, b, c, x := ids[0], ids[1], ids[2], ids[3]
	e.setPairs(t, gid, ring(a, b, c, x))
	e.discover(t, gid, c, x) // c perdra une cible déjà découverte

	p := e.preview(t, gid, remove(x))
	if p.Affected != 1 || p.AlreadyDiscovered != 1 {
		t.Fatalf("comptes inattendus : %+v", p)
	}
	if p.Summary != "1 personne verra sa cible changer, elle l'avait déjà découverte." {
		t.Fatalf("texte inattendu : %q", p.Summary)
	}
	check := func(v any) {
		walkValue(t, reflect.ValueOf(v), "aperçu", func(path, s string) {
			e.noNames(t, s)
			for _, mail := range e.emails {
				if strings.Contains(s, mail) {
					t.Fatalf("%s contient une adresse : %q", path, s)
				}
			}
		})
	}
	check(p)

	// Un aperçu sans découverte et un ajout.
	w := e.add(t, gid, "Nouveau", normal)
	p2 := e.preview(t, gid, addP(w))
	if p2.Affected != 1 || p2.AlreadyDiscovered != 0 ||
		p2.Summary != "1 personne verra sa cible changer, elle ne l'avait pas encore découverte." {
		t.Fatalf("aperçu inattendu : %+v", p2)
	}
	check(p2)

	// Le résultat appliqué ne contient pas plus de nom.
	r := e.apply(t, gid, remove(x))
	check(r)

	// Un refus ne nomme personne non plus : plus personne ne peut offrir à w.
	for _, id := range []int64{a, b, c} {
		if _, err := e.rules.AddExclusion(ctx, gid, id, w, false); err != nil {
			t.Fatal(err)
		}
	}
	p3 := e.preview(t, gid, addP(w))
	if p3.Possible || p3.Refusal == nil {
		t.Fatalf("refus attendu : %+v", p3)
	}
	check(p3)
	_, err := e.svc.Apply(ctx, admin(gid), gid, addP(w), Confirmation{Confirmed: true, Revision: p3.Revision})
	if err == nil {
		t.Fatal("refus attendu")
	}
	e.noNames(t, err.Error())
	var ref *Refusal
	if !errors.As(err, &ref) {
		t.Fatal("le refus doit porter son explication")
	}
	check(ref)
}

// Les textes de l'aperçu couvrent tous les cas de comptes.
func TestPreviewSummaryCounts(t *testing.T) {
	cases := map[[2]int]string{
		{0, 0}: "Aucune personne ne verra sa cible changer.",
		{1, 0}: "1 personne verra sa cible changer, elle ne l'avait pas encore découverte.",
		{1, 1}: "1 personne verra sa cible changer, elle l'avait déjà découverte.",
		{3, 0}: "3 personnes verront leur cible changer, aucune ne l'avait encore découverte.",
		{3, 1}: "3 personnes verront leur cible changer, dont 1 qui l'avait déjà découverte.",
		{3, 2}: "3 personnes verront leur cible changer, dont 2 qui l'avaient déjà découverte.",
		{2, 2}: "2 personnes verront leur cible changer, toutes l'avaient déjà découverte.",
	}
	for k, want := range cases {
		if got := summary(k[0], k[1]); got != want {
			t.Errorf("summary(%d, %d) = %q, attendu %q", k[0], k[1], got, want)
		}
	}
}
