package adminops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"secretsanta/internal/adminops"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// F10b.AC5 : l'import JSON est atomique ; un fichier valide recrée
// participants et foyers ; l'export puis l'import reproduit les mêmes données.
func TestExportImportRoundTripRecreatesParticipantsAndHouseholds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	src, _ := f.group(t, "famille-2026", "Famille 2026", "2026-12-24")
	a := f.person(t, src, "Élodie", "Martin", "elodie@exemple.fr", "")
	b := f.person(t, src, "Paul", "Martin", "", participants.RoleOffrePlus)
	c := f.person(t, src, "Zoé", "Durand", "zoe@exemple.fr", participants.RoleReceptionSeule)
	f.person(t, src, "Hugo", "Petit", "", participants.RoleUnSeul)
	if _, err := f.people.Update(ctx, participants.AdminActor(src), src, c, participants.Input{FirstName: "Zoé", Name: "Durand", Email: "zoe@exemple.fr", GiftHint: "Plantes\net thé"}); err != nil {
		t.Fatal(err)
	}
	if err := f.people.SetLocale(ctx, participants.AdminActor(src), src, a, "en"); err != nil {
		t.Fatal(err)
	}
	f.household(t, src, "Les Martin", a, b)
	f.seedDrawAndLetters(t, src, []int64{a, b, c})

	var buf bytes.Buffer
	if err := f.ops.ExportJSON(ctx, src, &buf); err != nil {
		t.Fatal(err)
	}
	// L'export ne contient ni paire, ni identifiant, ni lettre.
	for _, banned := range []string{"giver", "receiver", "pair", "\"id\"", "Cher Père Noël", "Un livre", "link", "code"} {
		if strings.Contains(buf.String(), banned) {
			t.Errorf("l'export contient %q", banned)
		}
	}

	dst, _ := f.group(t, "copie", "Copie", "")
	res, err := f.ops.Import(ctx, dst, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("import : %v", err)
	}
	if res.Participants != 4 || res.Households != 1 {
		t.Errorf("résultat %+v", res)
	}
	if got, want := roster(t, f.db, dst), roster(t, f.db, src); !reflect.DeepEqual(got, want) {
		t.Errorf("participants différents :\n%v\n%v", got, want)
	}
	if got, want := households(t, f.db, dst), households(t, f.db, src); !reflect.DeepEqual(got, want) || len(got) != 1 {
		t.Errorf("foyers différents : %v / %v", got, want)
	}
	// Rien du tirage ni des lettres n'est importé.
	if n := f.count(t, `SELECT COUNT(*) FROM draws WHERE group_id = ?`, dst); n != 0 {
		t.Errorf("%d tirages importés", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM letters WHERE group_id = ?`, dst); n != 0 {
		t.Errorf("%d lettres importées", n)
	}
	// Un second export du groupe importé est identique au premier.
	var again bytes.Buffer
	if err := f.ops.ExportJSON(ctx, dst, &again); err != nil {
		t.Fatal(err)
	}
	if again.String() != buf.String() {
		t.Errorf("export après import différent :\n%s\n---\n%s", again.String(), buf.String())
	}
}

func validFile(people ...adminops.ExchangePerson) adminops.ExchangeFile {
	return adminops.ExchangeFile{Format: adminops.ExchangeFormat, Version: adminops.ExchangeVersion, Participants: append([]adminops.ExchangePerson{}, people...),
		Households: []adminops.ExchangeHousehold{}}
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// F10b.AC5 : la moindre erreur du fichier n'importe rien, et la cause est
// nommée par un code du catalogue.
func TestInvalidImportImportsNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	gid, _ := f.group(t, "famille", "Famille", "")
	f.person(t, gid, "Anne", "Leroy", "", "")
	ok1 := adminops.ExchangePerson{FirstName: "Luc", Name: "Bernard"}
	ok2 := adminops.ExchangePerson{FirstName: "Inès", Name: "Moreau"}
	withHouse := validFile(ok1, ok2)
	withHouse.Households = []adminops.ExchangeHousehold{{Name: "Foyer", Members: []int{0, 5}}}
	twice := validFile(ok1, ok2)
	twice.Households = []adminops.ExchangeHousehold{{Name: "A", Members: []int{0}}, {Name: "B", Members: []int{0, 1}}}
	cases := map[string]struct {
		body []byte
		code string
	}{
		"pas du JSON":             {[]byte("ceci n'est pas du JSON"), adminops.CodeImportFormat},
		"champ inconnu":           {[]byte(`{"format":"secretsanta.participants","version":1,"participants":[{"first_name":"Luc","name":"B","pair":"x"}]}`), adminops.CodeImportFormat},
		"mauvais format":          {encode(t, adminops.ExchangeFile{Format: "autre", Version: 1, Participants: []adminops.ExchangePerson{ok1}}), adminops.CodeImportFormat},
		"mauvaise version":        {encode(t, adminops.ExchangeFile{Format: adminops.ExchangeFormat, Version: 2, Participants: []adminops.ExchangePerson{ok1}}), adminops.CodeImportFormat},
		"données en trop":         {append(encode(t, validFile(ok1)), []byte(` {}`)...), adminops.CodeImportFormat},
		"vide":                    {encode(t, validFile()), adminops.CodeImportEmpty},
		"prénom vide":             {encode(t, validFile(ok1, adminops.ExchangePerson{FirstName: "  ", Name: "X"})), adminops.CodeImportField},
		"nom trop long":           {encode(t, validFile(ok1, adminops.ExchangePerson{FirstName: "A", Name: strings.Repeat("é", 81)})), adminops.CodeImportField},
		"courriel invalide":       {encode(t, validFile(ok1, adminops.ExchangePerson{FirstName: "A", Name: "B", Email: "pas-une-adresse"})), adminops.CodeImportEmail},
		"langue invalide":         {encode(t, validFile(ok1, adminops.ExchangePerson{FirstName: "A", Name: "B", Locale: "!!"})), adminops.CodeImportLocale},
		"doublon dans le groupe":  {encode(t, validFile(ok1, adminops.ExchangePerson{FirstName: "anne", Name: "LEROY"})), adminops.CodeImportName},
		"doublon dans le fichier": {encode(t, validFile(ok1, ok2, adminops.ExchangePerson{FirstName: "Ines", Name: "moreau"})), adminops.CodeImportName},
		"foyer hors limites":      {encode(t, withHouse), adminops.CodeImportHouse},
		"personne dans 2 foyers":  {encode(t, twice), adminops.CodeImportHouse},
		"trop gros":               {append([]byte(`{"format":"`), bytes.Repeat([]byte("x"), adminops.MaxImportBytes)...), adminops.CodeImportTooLarge},
	}
	before := roster(t, f.db, gid)
	for name, c := range cases {
		_, err := f.ops.Import(ctx, gid, bytes.NewReader(c.body))
		if err == nil {
			t.Errorf("%s : import accepté", name)
			continue
		}
		if code, _ := errs.CodeOf(err); code != c.code {
			t.Errorf("%s : code %q, %q attendu (%v)", name, code, c.code, err)
		}
		if errs.KindOf(err) != errs.KindValidation {
			t.Errorf("%s : nature %s", name, errs.KindOf(err))
		}
		if got := roster(t, f.db, gid); !reflect.DeepEqual(got, before) {
			t.Fatalf("%s : des participants ont été importés : %v", name, got)
		}
		if n := f.count(t, `SELECT COUNT(*) FROM households WHERE group_id = ?`, gid); n != 0 {
			t.Fatalf("%s : %d foyers importés", name, n)
		}
	}
}

// Les saisies importées sont nettoyées comme à l'ajout manuel (F3).
func TestImportSanitizesInput(t *testing.T) {
	f := newFixture(t)
	gid, _ := f.group(t, "famille", "Famille", "")
	body := encode(t, validFile(adminops.ExchangePerson{FirstName: "  Jean​  ", Name: "Du\u0000pont   Moreau", GiftHint: "a\r\nb"}))
	if _, err := f.ops.Import(context.Background(), gid, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	got := roster(t, f.db, gid)
	if len(got) != 1 || got[0].First != "Jean" || got[0].Name != "Dupont Moreau" || got[0].Hint != "a\nb" || got[0].Role != "normal" {
		t.Errorf("saisie non nettoyée : %+v", got)
	}
}
