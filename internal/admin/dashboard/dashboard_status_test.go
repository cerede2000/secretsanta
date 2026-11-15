package dashboard

import (
	"html"
	"strconv"
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func TestDashboardBeforeDraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group("avant")
	e.addN(gid, "Hortensia", 3, participants.RoleNormal)
	v, err := e.svc.Dashboard(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if v.DrawDone || v.RevealDone || v.Registered != 3 || v.LinksOpened != 0 || v.TargetsSeen != 0 || len(v.People) != 3 {
		t.Fatalf("état avant tirage : %+v", v)
	}
	if _, err := e.svc.CheckDraw(ctx, admin(gid), gid); !errs.Is(err, errs.KindConflict) {
		t.Fatalf("contrôle sans tirage : %v", err)
	}
}

// F10.AC2 : tirage fait, ouvertures, révélation faite, lettre lue et statut des
// courriels, sans nom de cible.
func TestDashboardExposesStatusWithoutTargets(t *testing.T) {
	e := newEnv(t)
	gid := e.group("statut")
	a := e.add(gid, "Camille", "camille@exemple.fr", participants.RoleNormal)
	b := e.add(gid, "Hugo", "hugo@exemple.fr", participants.RoleNormal)
	c := e.add(gid, "Lea", "lea@exemple.fr", participants.RoleNormal)
	d := e.add(gid, "Mamie", "", participants.RoleNormal)
	e.doDraw(gid)

	e.mail(gid, a, "sent")
	e.mail(gid, b, "failed")
	e.mail(gid, c, "pending")
	// Ouvertures de lien : deux personnes se sont connectées.
	for _, id := range []int64{a, d} {
		if _, err := e.db.Exec(`INSERT INTO websec_sessions (id_hash, kind, group_id, subject, created_at, expires_at)
			VALUES (?, 'participant', ?, ?, 1, 2)`, "h"+strconv.FormatInt(id, 10), strconv.FormatInt(gid, 10), strconv.FormatInt(id, 10)); err != nil {
			t.Fatal(err)
		}
	}
	// Une session d'un autre groupe ne compte pas.
	if _, err := e.db.Exec(`INSERT INTO websec_sessions (id_hash, kind, group_id, subject, created_at, expires_at)
		VALUES ('autre', 'participant', '999', ?, 1, 2)`, strconv.FormatInt(b, 10)); err != nil {
		t.Fatal(err)
	}
	// Cibles découvertes par a seulement.
	if _, err := e.db.Exec(`UPDATE draw_pairs SET discovered_at = '2026-11-15T10:00:00Z' WHERE giver_id = ?`, a); err != nil {
		t.Fatal(err)
	}
	// Lettres : b et c ont écrit ; celle de b a été lue.
	for _, id := range []int64{b, c} {
		if _, err := e.db.Exec(`INSERT INTO letters (group_id, participant_id, version, created_at, updated_at) VALUES (?, ?, 1, 'x', 'x')`, gid, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.db.Exec(`INSERT INTO letter_reads (letter_id, version_read, read_at)
		SELECT id, 1, 'x' FROM letters WHERE participant_id = ?`, b); err != nil {
		t.Fatal(err)
	}
	e.reveal.open = true
	if err := e.svc.MarkLinkHandedOver(ctx, admin(gid), gid, d); err != nil {
		t.Fatal(err)
	}

	v, err := e.svc.Dashboard(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !v.DrawDone || !v.RevealDone || v.Registered != 4 {
		t.Fatalf("tirage ou révélation : %+v", v)
	}
	if v.LinksOpened != 2 {
		t.Errorf("liens ouverts : %d", v.LinksOpened)
	}
	if v.Targets != 4 || v.TargetsSeen != 1 {
		t.Errorf("cibles vues : %d sur %d", v.TargetsSeen, v.Targets)
	}
	if v.LettersWritten != 2 || v.LettersRead != 1 {
		t.Errorf("lettres : %d écrites, %d lues", v.LettersWritten, v.LettersRead)
	}
	if v.MailsSent != 1 || v.MailsFailed != 1 || v.MailsPending != 1 {
		t.Errorf("courriels : %d/%d/%d", v.MailsSent, v.MailsFailed, v.MailsPending)
	}
	if v.NoEmail != 1 || v.NoEmailHandedOver != 1 {
		t.Errorf("sans courriel : %d, remis : %d", v.NoEmail, v.NoEmailHandedOver)
	}
	by := map[int64]Person{}
	for _, p := range v.People {
		by[p.ID] = p
	}
	if by[a].Mail != MailSent || !by[a].LinkOpened || !by[a].TargetsSeen || by[b].Mail != MailFailed ||
		!by[b].LetterWritten || !by[b].LetterRead || by[c].LetterRead || !by[d].HandedOver || by[d].HasEmail {
		t.Errorf("lignes : %+v", by)
	}

	// La page affiche ces faits, en français et en anglais.
	ck := e.adminCookie(gid)
	fr := html.UnescapeString(e.get("/statut/admin", "fr", ck).Body.String())
	for _, want := range []string{"Échec d'envoi", "Courriel envoyé", "Lien ouvert", "Cible découverte", "Lettre lue", "Sans mail, lien remis", "Contrôler le tirage"} {
		if !strings.Contains(fr, want) {
			t.Errorf("la page ne dit pas %q", want)
		}
	}
	en := html.UnescapeString(e.get("/statut/admin", "en", ck).Body.String())
	for _, want := range []string{"Sending failed", "Email sent", "Link opened", "Target discovered", "Letter read"} {
		if !strings.Contains(en, want) {
			t.Errorf("la page anglaise ne dit pas %q", want)
		}
	}
}

func TestHandoverIsAuditedAndOnlyForPeopleWithoutEmail(t *testing.T) {
	e := newEnv(t)
	gid := e.group("remise")
	withMail := e.add(gid, "Camille", "camille@exemple.fr", participants.RoleNormal)
	without := e.add(gid, "Mamie", "", participants.RoleNormal)
	if err := e.svc.MarkLinkHandedOver(ctx, admin(gid), gid, withMail); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("personne avec courriel : %v", err)
	}
	if err := e.svc.MarkLinkHandedOver(ctx, admin(gid), gid, 4242); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("personne inconnue : %v", err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := e.svc.MarkLinkHandedOver(ctx, admin(gid), gid, without); err != nil {
			t.Fatal(err)
		}
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM admin_link_handover WHERE group_id = ?`, gid); n != 1 {
		t.Fatalf("marques : %d", n)
	}
	log, err := e.audit.List(ctx, admin(gid), gid, 10)
	if err != nil || len(log) == 0 || log[0].Action != "lien_remis" || log[0].ParticipantID != without {
		t.Fatalf("journal : %+v %v", log, err)
	}
}
