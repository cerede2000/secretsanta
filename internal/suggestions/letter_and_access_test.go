package suggestions

import (
	"strings"
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/sources"
)

// F8c.AC2 : les suggestions s'appuient sur la lettre du tiré, mais seul son
// tireur y a accès.
func TestLetterKeywordsUsedForGiverOnly(t *testing.T) {
	e := newEnv(t, Options{})
	e.letters.by[2] = &letter.Letter{Version: 1, Ideas: []letter.Idea{{Title: "Théière japonaise", Text: "du thé vert"}}}
	e.search.res = []sources.Suggestion{sug("a", "10 €")}

	r, err := e.svc.Suggest(ctx, e.req(1))
	if err != nil || !r.FromLetter {
		t.Fatalf("lettre non utilisée : %v %v", r, err)
	}
	q := strings.Join(e.search.queries[0].Keywords, " ")
	if !strings.Contains(q, "theiere") || !strings.Contains(q, "japonaise") {
		t.Fatalf("mots-clés de la lettre absents : %q", q)
	}

	// Les autres : le tiré lui-même, une autre personne, l'administrateur, un anonyme, un autre groupe.
	for name, a := range map[string]participants.Actor{
		"tiré":         participants.ParticipantActor(e.gid, 2),
		"autre":        participants.ParticipantActor(e.gid, 3),
		"admin":        participants.AdminActor(e.gid),
		"anonyme":      {},
		"autre groupe": participants.ParticipantActor(e.gid+1, 1),
	} {
		req := Request{Actor: a, GroupID: e.gid}
		before := e.search.calls()
		res, err := e.svc.Suggest(ctx, req)
		if err == nil {
			t.Fatalf("%s : accès accordé (%v)", name, res)
		}
		if k := errs.KindOf(err); k != errs.KindForbidden && k != errs.KindNotFound {
			t.Fatalf("%s : nature %s", name, k)
		}
		if e.search.calls() != before {
			t.Fatalf("%s : une recherche est partie", name)
		}
	}
}

// Sans lettre, une recherche générique part sans mot-clé.
func TestNoLetterGenericSearch(t *testing.T) {
	e := newEnv(t, Options{})
	e.search.res = []sources.Suggestion{sug("a", "")}
	r, err := e.svc.Suggest(ctx, e.req(1))
	if err != nil || r.FromLetter || len(e.search.queries[0].Keywords) != 0 {
		t.Fatalf("%v %v %v", r, err, e.search.queries)
	}
}
