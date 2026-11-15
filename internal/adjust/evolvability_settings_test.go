package adjust

import (
	"strconv"
	"testing"

	accessmig "secretsanta/internal/access/migrations"
	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/db"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// F4c.AC6 : après le tirage, modifier le budget (défini, modifié, retiré puis
// redéfini), la date du jour J, la consigne globale, les indices cadeau, les
// noms, prénoms et adresses ne change aucune paire, aucun drapeau de
// révélation ni aucun lien personnel.
func TestSettingsAfterDrawKeepPairsFlagsAndLinks(t *testing.T) {
	e := newEnv(t,
		func() (string, []db.Migration, error) {
			m, err := groupsettings.Migrations()
			return groupsettings.Module, m, err
		},
		func() (string, []db.Migration, error) {
			m, err := accessmig.Migrations()
			return accessmig.Module, m, err
		},
	)
	gid := e.group(t, "famille", 2)
	ids := e.people(t, gid, normal, normal, normal, normal)
	e.setPairs(t, gid, ring(ids...))
	e.discover(t, gid, ids[0], ids[1])
	e.discover(t, gid, ids[2], ids[3])

	tokens, err := token.NewService(sqlstore.New(e.db), token.LoginCodeLength, []byte("cle-de-signature-de-test-32-octets!!"))
	if err != nil {
		t.Fatal(err)
	}
	gs := strconv.FormatInt(gid, 10)
	links := map[int64]string{}
	codes := map[int64]string{}
	for _, id := range ids {
		ps := strconv.FormatInt(id, 10)
		if links[id], err = tokens.IssueLink(ctx, gs, ps); err != nil {
			t.Fatal(err)
		}
		if codes[id], err = tokens.IssueCode(ctx, gs, ps); err != nil {
			t.Fatal(err)
		}
	}
	before := e.snapshot(t, gid)

	settings := groupsettings.NewService(e.db, e.clock, &ports.MemoryEventBus{}, e.draw.State, nil)
	steps := []groupsettings.Input{
		{GroupID: gid, Name: "Famille", Budget: "25"},                                                                // défini
		{GroupID: gid, Name: "Famille", Budget: "30,50"},                                                             // modifié
		{GroupID: gid, Name: "Famille", Budget: ""},                                                                  // retiré
		{GroupID: gid, Name: "Famille", Budget: "20", EventDate: "2026-12-24"},                                       // redéfini, jour J
		{GroupID: gid, Name: "Famille Noël", Budget: "20", EventDate: "2026-12-25", GiftInstructions: "Fait main !"}, // consigne
	}
	for i, in := range steps {
		res, err := settings.UpdateGroupSettings(ctx, in)
		if err != nil {
			t.Fatalf("étape %d : %v", i, err)
		}
		if i < 4 && !res.BudgetChanged {
			t.Fatalf("étape %d : le budget aurait dû changer", i)
		}
		e.unchangedPairs(t, gid, before)
	}
	for i, id := range ids {
		in := participants.Input{
			FirstName: "Nouveau" + strconv.Itoa(i), Name: "Renommé" + strconv.Itoa(i),
			Email: "autre" + strconv.Itoa(i) + "@exemple.fr", GiftHint: "Plutôt des livres",
		}
		if _, err := e.parts.Update(ctx, admin(gid), gid, id, in); err != nil {
			t.Fatal(err)
		}
		e.unchangedPairs(t, gid, before)
	}
	for _, id := range ids {
		got, err := tokens.ResolveLink(ctx, gs, links[id])
		if err != nil || got != strconv.FormatInt(id, 10) {
			t.Fatalf("lien personnel de %d cassé : %q, %v", id, got, err)
		}
		got, err = tokens.ResolveCode(ctx, gs, codes[id])
		if err != nil || got != strconv.FormatInt(id, 10) {
			t.Fatalf("code personnel de %d cassé : %q, %v", id, got, err)
		}
	}
	if e.count(t, `SELECT COUNT(*) FROM draws WHERE group_id = ?`, gid) != 1 {
		t.Fatal("un tirage a été créé ou archivé")
	}
}

// unchangedPairs vérifie que les paires (identifiants de ligne compris),
// leurs drapeaux, la révision et l'historique du tirage n'ont pas bougé.
func (e *env) unchangedPairs(t *testing.T, gid int64, before snapshot) {
	t.Helper()
	after := e.snapshot(t, gid)
	if len(after.rows) != len(before.rows) || after.revision != before.revision || after.events != before.events {
		t.Fatalf("le tirage a changé : %+v → %+v", before, after)
	}
	for i := range after.rows {
		if after.rows[i] != before.rows[i] {
			t.Fatalf("paire ou drapeau modifié : %+v → %+v", before.rows[i], after.rows[i])
		}
	}
}
