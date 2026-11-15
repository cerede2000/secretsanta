package suggestions

import (
	"strings"
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/sources"
)

// F8c.AC3 : les mots-clés envoyés écartent noms, prénoms et courriels, même
// quand la lettre les contient.
func TestKeywordsDropPersonalData(t *testing.T) {
	e := newEnv(t, Options{})
	e.letters.by[2] = &letter.Letter{Version: 1, Ideas: []letter.Idea{
		{Title: "Livre pour Alice", Text: "écrire à bob.martin@example.org ou Zxchloeovitch ; Chloé aime la randonnée"},
		{Title: "Contact : dan@exemple.fr", Text: "ZXALICEOVITCH, Zxbobovitch et jumelles https://perso.example/zxdan"},
	}}
	e.search.res = []sources.Suggestion{sug("a", "")}
	if _, err := e.svc.Suggest(ctx, e.req(1)); err != nil {
		t.Fatal(err)
	}
	q := e.search.queries[0]
	all := strings.ToLower(strings.Join(q.Keywords, " "))
	for _, bad := range []string{"alice", "bob", "chloe", "chloé", "dan", "zx", "@", "example", "exemple", "martin", "perso"} {
		if strings.Contains(all, bad) {
			t.Fatalf("donnée personnelle %q dans %q", bad, all)
		}
	}
	if !strings.Contains(all, "livre") || !strings.Contains(all, "randonnee") {
		t.Fatalf("mots utiles perdus : %q", all)
	}
}

func TestKeywordsFunction(t *testing.T) {
	got := Keywords([]string{"A.B@x.org, jean-pierre un Vélo électrique 2024"}, []string{"Jean-Pierre Dupont"})
	want := "velo electrique"
	if strings.Join(got, " ") != want {
		t.Fatalf("%v", got)
	}
}

// La base ne garde pas les mots de la lettre.
func TestCacheKeepsNoLetterWords(t *testing.T) {
	e := newEnv(t, Options{})
	e.letters.by[2] = &letter.Letter{Version: 1, Ideas: []letter.Idea{{Title: "aquarelle"}}}
	e.search.res = []sources.Suggestion{sug("a", "")}
	if _, err := e.svc.Suggest(ctx, e.req(1)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM suggestion_cache WHERE keywords_key LIKE '%aquarelle%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("mot de la lettre en base : %d %v", n, err)
	}
}
