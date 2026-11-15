package targetpage

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

func pairIDs(t *testing.T, e *env, gid, pid int64) (pairs, receivers []int64) {
	t.Helper()
	mine, err := e.draw.MyTargets(ctx, participants.ParticipantActor(gid, pid), gid)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range mine.Targets {
		pairs = append(pairs, tg.PairID)
		receivers = append(receivers, tg.ReceiverID)
	}
	return
}

func cardLinks(slug string, pair int64) (letter, ideas string) {
	return "/" + slug + "/lettre-recue/" + itoa(pair), "/" + slug + "/suggestions/" + itoa(pair)
}

// Deux cibles : la seconde reste « à découvrir », sans nom ni lien, tant que
// son drapeau n'est pas posé ; chaque cible découverte a ses liens.
func TestTwoTargetsSecondStaysToDiscover(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "deux-cibles", nil, true)
	giver := s.more[0]
	pairs, recv := pairIDs(t, e, s.gid, giver)
	if len(pairs) != 2 {
		t.Fatalf("%d cible(s), 2 attendues", len(pairs))
	}
	n1, n2 := e.firstName(t, recv[0]), e.firstName(t, recv[1])
	e.login(t, s.gid, giver)

	// Aucune cible découverte : renvoi vers la découverte, rien n'est rendu.
	w := e.do("GET", "/"+s.slug+"/ma-cible", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/"+s.slug+"/decouverte" {
		t.Fatalf("avant découverte : %d %q", w.Code, w.Header().Get("Location"))
	}
	mustNotContain(t, w.Body.String(), n1)

	// Première cible découverte seulement.
	e.discover(t, s.gid, giver, 0)
	w = e.do("GET", "/"+s.slug+"/ma-cible", "", nil)
	if w.Code != 200 {
		t.Fatalf("carte : %d", w.Code)
	}
	body := w.Body.String()
	mustContain(t, body, n1)
	mustContain(t, body, "Cadeau 1 sur 2")
	mustContain(t, body, "Cadeau 2 sur 2")
	mustContain(t, body, "À découvrir")
	mustContain(t, body, "Indice de "+n1)
	l1, i1 := cardLinks(s.slug, pairs[0])
	l2, i2 := cardLinks(s.slug, pairs[1])
	mustContain(t, body, `href="`+l1+`"`)
	mustContain(t, body, `href="`+i1+`"`)
	mustNotContain(t, body, n2)
	mustNotContain(t, body, "Indice de "+n2)
	mustNotContain(t, body, l2)
	mustNotContain(t, body, i2)
	mustContain(t, body, `href="/`+s.slug+`/decouverte"`)

	// Les deux découvertes : deux cartes, quatre liens.
	e.discover(t, s.gid, giver, 1)
	body = e.do("GET", "/"+s.slug+"/ma-cible", "", nil).Body.String()
	for _, want := range []string{n1, n2, l1, i1, l2, i2} {
		mustContain(t, body, want)
	}
	mustNotContain(t, body, "À découvrir")
	if got := strings.Count(body, `class="draw-card"`); got != 2 {
		t.Errorf("%d carte(s), 2 attendues", got)
	}
}

// Une seule cible : une seule carte, sans pastille « Cadeau n sur n ».
func TestSingleTargetCard(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "une-cible", nil, true)
	pid := s.normal[0]
	pairs, recv := pairIDs(t, e, s.gid, pid)
	if len(pairs) != 1 {
		t.Fatalf("%d cible(s), 1 attendue", len(pairs))
	}
	e.discover(t, s.gid, pid, -1)
	e.login(t, s.gid, pid)
	body := e.do("GET", "/"+s.slug+"/participant", "", nil).Body.String()
	letter, ideas := cardLinks(s.slug, pairs[0])
	for _, want := range []string{e.firstName(t, recv[0]), letter, ideas, "Tu offres un cadeau à"} {
		mustContain(t, body, want)
	}
	mustNotContain(t, body, "Cadeau 1 sur")
	if got := strings.Count(body, `class="draw-card"`); got != 1 {
		t.Errorf("%d carte(s), 1 attendue", got)
	}
}

// Une personne ne voit jamais la cible d'une autre.
func TestCardShowsOnlyOwnTargets(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "cartes-privees", nil, true)
	a, b := s.normal[0], s.normal[1]
	_, recvB := pairIDs(t, e, s.gid, b)
	_, recvA := pairIDs(t, e, s.gid, a)
	e.discover(t, s.gid, a, -1)
	e.discover(t, s.gid, b, -1)
	e.login(t, s.gid, a)
	body := e.do("GET", "/"+s.slug+"/ma-cible", "", nil).Body.String()
	mustContain(t, body, e.firstName(t, recvA[0]))
	for _, id := range recvB {
		if id != recvA[0] {
			mustNotContain(t, body, e.firstName(t, id))
		}
	}
}

// Avant l'ouverture de la révélation, aucune cible n'est rendue, même
// découverte par erreur en base.
func TestNoTargetBeforeReveal(t *testing.T) {
	e := newEnv(t)
	s := newScene(t, e, "cible-fermee", at(72*3600*1e9), true)
	pid := s.normal[0]
	_, recv := pairIDs(t, e, s.gid, pid)
	if len(recv) != 0 {
		t.Fatalf("MyTargets renvoie %d cible(s) avant l'ouverture", len(recv))
	}
	e.login(t, s.gid, pid)
	body := e.do("GET", "/"+s.slug+"/ma-cible", "", nil).Body.String()
	mustNotContain(t, body, "draw-card")
	mustNotContain(t, body, "Tu offres un cadeau à")
	mustContain(t, body, "Le tirage est prêt")
}
