package reveal

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// AC8 : « ouvrir maintenant » exige une confirmation et fait passer la
// révélation à l'état ouvert pour tous.
func TestOpenNowRequiresConfirmationAndOpensForEveryone(t *testing.T) {
	e := newEnv(t)
	at := t0.Add(10 * 24 * time.Hour)
	gid := e.group(t, "ouvrir", &at)
	other := e.group(t, "voisins", nil)
	ids := e.addN(t, gid, "Seraphine", 2, participants.RoleNormal)
	ids = append(ids, e.addN(t, gid, "Anatole", 2, participants.RoleOffrePlus)...)
	ids = append(ids, e.add(t, gid, "Rosalinde", participants.RoleReceptionSeule))

	// Avant le tirage : rien à ouvrir.
	_, err := e.svc.OpenNow(ctx, admin(gid), gid, true)
	e.wantKind(t, err, errs.KindConflict)

	o := e.mustDraw(t, gid)
	e.wantClosed(t, gid, ids)

	// Sans confirmation : refusé, rien ne change.
	_, err = e.svc.OpenNow(ctx, admin(gid), gid, false)
	msg := e.wantKind(t, err, errs.KindValidation)
	wantFrench(t, msg)
	e.wantClosed(t, gid, ids)

	// Seul l'administrateur du groupe peut ouvrir.
	_, err = e.svc.OpenNow(ctx, participants.Actor{}, gid, true)
	e.wantKind(t, err, errs.KindUnauthorized)
	_, err = e.svc.OpenNow(ctx, who(gid, ids[0]), gid, true)
	e.wantKind(t, err, errs.KindForbidden)
	_, err = e.svc.OpenNow(ctx, admin(other), gid, true)
	e.wantKind(t, err, errs.KindForbidden)
	e.wantClosed(t, gid, ids)
	if n := e.count(t, `SELECT COUNT(*) FROM reveal_openings`); n != 0 {
		t.Fatalf("%d ouverture(s) enregistrée(s) sans confirmation", n)
	}
	if n := len(e.mail.notices()); n != 0 {
		t.Fatalf("%d avis partis sans ouverture", n)
	}

	// Confirmé : ouvert pour tous, tout de suite, bien avant la date.
	opened, err := e.svc.OpenNow(ctx, admin(gid), gid, true)
	if err != nil || !opened {
		t.Fatalf("ouverture confirmée : %v %v", opened, err)
	}
	e.wantOpen(t, gid, o.DrawID, ids)
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("%d avis « tirage prêt », %d attendus", n, len(ids))
	}
	f, err := e.svc.Facts(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Open || f.OpenedHow != HowManual || f.OpenedAt == nil || !f.OpenedAt.Equal(t0) || !f.DateLocked {
		t.Fatalf("faits après ouverture : %+v", f)
	}
	noPairType(t, f)

	// Un second appel ne change rien et n'envoie rien de plus.
	opened, err = e.svc.OpenNow(ctx, admin(gid), gid, true)
	if err != nil || opened {
		t.Fatalf("seconde ouverture : %v %v", opened, err)
	}
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("avis en double : %d", n)
	}

	// La date arrive : la révélation reste ouverte, sans nouvel avis.
	e.clock.Set(at)
	e.tick(t)
	e.wantOpen(t, gid, o.DrawID, ids)
	if n := len(e.mail.notices()); n != len(ids) {
		t.Fatalf("avis en double à la date : %d", n)
	}
	f, _ = e.svc.Facts(ctx, admin(gid), gid)
	if f.OpenedHow != HowManual {
		t.Fatalf("l'ouverture a été réécrite : %+v", f)
	}
	if _, err := e.db.Exec(`UPDATE reveal_openings SET how = 'date'`); err == nil {
		t.Fatal("une ouverture a pu être modifiée")
	}

	// L'autre groupe reste intact.
	if n := e.count(t, `SELECT COUNT(*) FROM reveal_openings WHERE group_id = ?`, other); n != 0 {
		t.Fatal("l'ouverture a touché un autre groupe")
	}
}
