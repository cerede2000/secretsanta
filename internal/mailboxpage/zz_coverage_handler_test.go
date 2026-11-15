package mailboxpage

import (
	"net/http"
	"testing"
)

func TestZZHandlerRefusalsAndFailures(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "boite")
	pid := s.normal[0]
	url := "/" + s.slug + "/boite"

	if w := e.post(t, s.slug, "/boite", "", nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST : %d", w.Code)
	}
	if w := e.do("GET", "/inconnu/boite", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", w.Code)
	}
	if w := e.do("GET", url, "", nil); w.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", w.Code)
	}
	if w := e.do("HEAD", url, "", nil); w.Code != http.StatusSeeOther {
		t.Errorf("HEAD sans session : %d", w.Code)
	}
	// Sujet de session invalide : renvoi vers la connexion.
	e.login(t, s.gid, 0)
	if w := e.do("GET", url, "", nil); w.Code != http.StatusSeeOther {
		t.Errorf("sujet nul : %d", w.Code)
	}
	// Personne supprimée : renvoi vers la connexion.
	e.login(t, s.gid, 987654)
	if w := e.do("GET", url, "", nil); w.Code != http.StatusSeeOther {
		t.Errorf("personne inconnue : %d", w.Code)
	}
	e.login(t, s.gid, pid)
	e.discover(t, s.gid, pid, -1)
	if w := e.do("GET", url, "", nil); w.Code != 200 {
		t.Fatalf("page : %d", w.Code)
	}

	for name, q := range map[string]string{
		"stamps":  `DROP TABLE letterread_stamps`,
		"letters": `DROP TABLE letters`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			s := newScene(t, e, "boite")
			pid := s.normal[0]
			if _, err := e.letters.AddIdea(ctx, s.gid, pid, ideaInput("x")); err != nil {
				t.Fatal(err)
			}
			e.login(t, s.gid, pid)
			e.discover(t, s.gid, pid, -1)
			if _, err := e.db.Exec(q); err != nil {
				t.Skipf("%s : %v", q, err)
			}
			if w := e.do("GET", "/"+s.slug+"/boite", "", nil); w.Code != http.StatusInternalServerError {
				t.Errorf("panne : %d", w.Code)
			}
		})
	}
	t.Run("tirage", func(t *testing.T) {
		e := newEnv(t)
		s := newScene(t, e, "boite")
		e.login(t, s.gid, s.normal[0])
		if _, err := e.db.Exec(`DROP TRIGGER draw_pairs_immutable`); err != nil {
			t.Fatal(err)
		}
		if _, err := e.db.Exec(`DROP TABLE draw_pairs`); err != nil {
			t.Fatal(err)
		}
		if w := e.do("GET", "/"+s.slug+"/boite", "", nil); w.Code != http.StatusInternalServerError {
			t.Errorf("panne du tirage : %d", w.Code)
		}
	})
}
