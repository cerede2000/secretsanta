package mailboxpage

import "testing"

// Une personne en réception seule ne voit que « Ma lettre ».
func TestReceptionOnlyMailboxHasOnlyMyLetter(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "reception")
	for _, id := range s.reception {
		e.login(t, s.gid, id)
		w := e.do("GET", "/"+s.slug+"/boite", "", nil)
		if w.Code != 200 {
			t.Fatalf("statut %d", w.Code)
		}
		body := w.Body.String()
		mustContain(t, body, "Ma lettre au Père Noël")
		mustNotContain(t, body, "Lettre reçue")
		if n := len(recvHrefRe.FindAllString(body, -1)); n != 0 {
			t.Errorf("%d lettre(s) reçue(s) pour une personne en réception seule", n)
		}
		mustNotContain(t, body, "/lettre-recue/")
		mustContain(t, body, "/"+s.slug+"/lettre\"")
	}
}

// Avant la révélation (ou sans découverte), aucune lettre reçue non plus.
func TestNoReceivedLetterBeforeDiscovery(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "avant-decouverte")
	e.login(t, s.gid, s.more[0])
	body := e.do("GET", "/"+s.slug+"/boite", "", nil).Body.String()
	mustContain(t, body, "Ma lettre au Père Noël")
	mustNotContain(t, body, "/lettre-recue/")
}
