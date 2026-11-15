package verify

import (
	"testing"

	"secretsanta/internal/participants"
)

// F11.AC15 : une inscription en attente n'a ni lien ni code personnel,
// n'apparaît pas parmi les participants, n'est pas éligible au tirage, mais
// est comptée par RegistrationState.
func TestPendingRegistrationIsNotAParticipant(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	e.register(t, e.g1, "Inès", "Petit", "ines@example.org")

	// Ni lien ni code personnel (F2b) : aucun secret d'accès émis.
	if n := e.credentialRows(t); n != 0 {
		t.Fatalf("%d secrets personnels pour des inscriptions en attente", n)
	}
	// Pas de participant, ni pour l'administrateur ni pour le tirage.
	if ps := e.participants(t, e.g1); len(ps) != 0 {
		t.Fatalf("participants : %+v", ps)
	}
	roster, err := e.parts.Roster(ctx, e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster.Entries) != 0 || roster.Counts != (participants.RoleCounts{}) {
		t.Fatalf("liste du tirage : %+v", roster)
	}
	canDraw, err := e.parts.CanDraw(ctx, e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if canDraw {
		t.Fatal("trois inscriptions en attente rendent le tirage possible")
	}
	// Mais RegistrationState les compte, dans leur groupe seulement.
	if n := e.pendingCount(t, e.g1); n != 3 {
		t.Fatalf("PendingCount = %d, 3 attendu", n)
	}
	if n := e.pendingCount(t, e.g2); n != 0 {
		t.Fatalf("PendingCount de l'autre groupe = %d", n)
	}
	// Seul le code de vérification est parti, et aucun courriel de bienvenue.
	if e.codeMail.count() != 3 || len(e.welcome.all()) != 0 {
		t.Fatalf("courriels : %d codes, %d bienvenues", e.codeMail.count(), len(e.welcome.all()))
	}
}
