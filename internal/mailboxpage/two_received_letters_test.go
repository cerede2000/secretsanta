package mailboxpage

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

var recvHrefRe = regexp.MustCompile(`href="(/[^"]*/lettre-recue/(\d+))"`)

func TestTwoTargetsListOneReceivedLetterPerDiscoveredTarget(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "deux-lettres")
	giver := s.more[0]
	targets := e.discover(t, s.gid, giver, -1)
	if len(targets) != 2 {
		t.Fatalf("%d cibles, 2 attendues", len(targets))
	}
	intros := []string{"Mot-du-premier-tire", "Mot-du-second-tire"}
	for i, tg := range targets {
		if _, err := e.letters.SetIntro(ctx, s.gid, tg.ReceiverID, intros[i]); err != nil {
			t.Fatal(err)
		}
	}
	e.login(t, s.gid, giver)
	body := e.do("GET", "/"+s.slug+"/boite", "", nil).Body.String()
	mustContain(t, body, "Lettre reçue · cadeau 1 sur 2")
	mustContain(t, body, "Lettre reçue · cadeau 2 sur 2")
	mustContain(t, body, "Ma lettre au Père Noël")
	links := recvHrefRe.FindAllStringSubmatch(body, -1)
	if len(links) != 2 {
		t.Fatalf("%d liens de lettre reçue, 2 attendus", len(links))
	}
	if links[0][1] == links[1][1] {
		t.Fatal("les deux entrées pointent vers la même lettre")
	}
	a := participants.ParticipantActor(s.gid, giver)
	for i, l := range links {
		if l[1] != "/"+s.slug+"/lettre-recue/"+itoa(targets[i].PairID) {
			t.Errorf("entrée %d : %s, paire %d attendue", i+1, l[1], targets[i].PairID)
		}
		pair, _ := strconv.ParseInt(l[2], 10, 64)
		got, err := e.read.Read(ctx, a, s.gid, pair)
		if err != nil {
			t.Fatalf("lecture de la lettre %d : %v", i+1, err)
		}
		if got.Intro != intros[i] {
			t.Errorf("lettre %d : %q, %q attendu (lettres mélangées)", i+1, got.Intro, intros[i])
		}
	}
	// Aucun nom, aucun indice, aucun contenu de lettre dans la boîte.
	for _, tg := range targets {
		var first string
		_ = e.db.QueryRow(`SELECT first_name FROM participants WHERE id = ?`, tg.ReceiverID).Scan(&first)
		mustNotContain(t, body, first)
	}
	for _, in := range intros {
		mustNotContain(t, body, in)
	}
}

// Seule la cible découverte est listée ; l'autre reste invisible.
func TestUndiscoveredTargetNotListed(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "une-decouverte")
	giver := s.more[0]
	targets := e.discover(t, s.gid, giver, 1)
	e.login(t, s.gid, giver)
	body := e.do("GET", "/"+s.slug+"/boite", "", nil).Body.String()
	links := recvHrefRe.FindAllStringSubmatch(body, -1)
	if len(links) != 1 || !strings.HasSuffix(links[0][1], "/"+itoa(targets[1].PairID)) {
		t.Fatalf("liens : %v", links)
	}
	mustContain(t, body, "cadeau 2 sur 2")
	mustNotContain(t, body, "cadeau 1 sur 2")
}

// Avec une seule cible : « Lettre reçue » sans numéro.
func TestSingleTargetHasNoNumber(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "une-cible")
	e.discover(t, s.gid, s.normal[0], -1)
	e.login(t, s.gid, s.normal[0])
	body := e.do("GET", "/"+s.slug+"/boite", "", nil).Body.String()
	if n := len(recvHrefRe.FindAllString(body, -1)); n != 1 {
		t.Fatalf("%d lettres reçues", n)
	}
	mustNotContain(t, body, "sur 2")
}

// États de la lettre de la personne : vide, brouillon, envoyée, lue.
func TestOwnLetterStates(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "etats")
	me := s.normal[0]
	e.login(t, s.gid, me)
	get := func() string {
		w := e.do("GET", "/"+s.slug+"/boite", "", nil)
		if w.Code != 200 {
			t.Fatalf("statut %d", w.Code)
		}
		return w.Body.String()
	}
	body := get()
	mustContain(t, body, `data-state="empty"`)
	mustContain(t, body, "Pas encore écrite")
	if _, err := e.letters.SetIntro(ctx, s.gid, me, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.letters.AddIdea(ctx, s.gid, me, ideaInput("Mug")); err != nil {
		t.Fatal(err)
	}
	mustContain(t, get(), `data-state="draft"`)
	if _, err := e.letters.SetIntro(ctx, s.gid, me, "Cher Père Noël"); err != nil {
		t.Fatal(err)
	}
	body = get()
	mustContain(t, body, `data-state="sent"`)
	mustContain(t, body, "Pas encore lue")
	// Le tireur lit la lettre : cachet « lue ».
	for _, g := range []int64{s.normal[1], s.more[0], s.more[1]} {
		for _, tg := range e.discover(t, s.gid, g, -1) {
			if tg.ReceiverID == me {
				if _, err := e.read.Read(ctx, participants.ParticipantActor(s.gid, g), s.gid, tg.PairID); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	body = get()
	mustContain(t, body, `data-state="read"`)
	mustContain(t, body, "Lue par le Père Noël")
}
