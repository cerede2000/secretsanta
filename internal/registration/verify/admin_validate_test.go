package verify

import (
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/shared/errs"
)

// F11.AC17 : « Valider sans code » crée le participant par le même chemin,
// envoie la bienvenue, journalise l'action (administrateur, date,
// inscription), ne montre aucune paire ; une saisie ultérieure du code mène
// à l'écran de réussite sans doublon.
func TestAdminValidateWithoutCode(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	e.clock.Advance(2 * time.Minute)
	at := e.clock.Now()

	v, err := e.svc.ValidateWithoutCode(ctx, admin(e.g1), e.g1, r.id)
	if err != nil {
		t.Fatal(err)
	}
	if v.RegistrationID != r.id || v.ParticipantID == 0 || !v.ValidatedAt.Equal(at) || v.FirstName != "Léa" {
		t.Fatalf("validation %+v", v)
	}
	ps := e.participants(t, e.g1)
	if len(ps) != 1 || ps[0].ID != v.ParticipantID || !ps[0].JoinedAt.Equal(at) {
		t.Fatalf("participants %+v", ps)
	}
	if n := e.credentialRows(t); n != 2 {
		t.Fatalf("%d secrets personnels, 2 attendus", n)
	}
	ws := e.welcome.all()
	if len(ws) != 1 || ws[0].ParticipantID != v.ParticipantID || ws[0].Link == "" {
		t.Fatalf("bienvenue %+v", ws)
	}
	if e.pendingCount(t, e.g1) != 0 {
		t.Fatal("l'inscription est encore en attente")
	}

	// Journal : administrateur, date, inscription.
	log, err := e.svc.Audit(ctx, admin(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 {
		t.Fatalf("journal %+v", log)
	}
	l := log[0]
	if l.Action != ActionValidate || l.Actor != "administrateur" || !l.At.Equal(at) ||
		l.RegistrationID != r.id || l.ParticipantID != v.ParticipantID || l.GroupID != e.g1 {
		t.Fatalf("ligne de journal %+v", l)
	}
	if other, _ := e.svc.Audit(ctx, admin(e.g2), e.g2); len(other) != 0 {
		t.Fatal("le journal fuit vers un autre groupe")
	}

	// Aucune paire dans ce que voit l'administrateur.
	noPairFields(t, v)
	noPairFields(t, log)
	noPairFields(t, &Success{})

	// La personne saisit ensuite son code : écran de réussite, sans doublon.
	e.clock.Advance(time.Minute)
	ok, err := e.svc.VerifyCode(ctx, e.g1, r.ref, e.code(t, r))
	if err != nil {
		t.Fatalf("saisie après validation : %v", err)
	}
	if !ok.AlreadyVerified || ok.ParticipantID != v.ParticipantID || ok.Slug != "noel" {
		t.Fatalf("réussite %+v", ok)
	}
	if len(e.participants(t, e.g1)) != 1 || len(e.welcome.all()) != 1 {
		t.Fatal("doublon après la saisie du code")
	}
	// Un code faux ne mène pas à la réussite pour autant.
	r2 := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	if _, err := e.svc.ValidateWithoutCode(ctx, admin(e.g1), e.g1, r2.id); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.VerifyCode(ctx, e.g1, r2.ref, "222222")
	mustRefused(t, err)
	// Le lien en un clic aussi retombe sur la réussite.
	ok, err = e.svc.ConfirmLink(ctx, e.g1, e.confirmToken(t, r2))
	if err != nil || !ok.AlreadyVerified {
		t.Fatalf("lien après validation : %+v %v", ok, err)
	}
	if len(e.participants(t, e.g1)) != 2 {
		t.Fatal("doublon par le lien en un clic")
	}
	// Une seconde validation par l'administrateur ne crée rien.
	_, err = e.svc.ValidateWithoutCode(ctx, admin(e.g1), e.g1, r.id)
	if !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("seconde validation : %v", err)
	}
}

// Seul l'administrateur du groupe valide ; jamais après le tirage.
func TestAdminValidateIsGuarded(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")

	for _, a := range []participants.Actor{
		{}, admin(e.g2), participants.ParticipantActor(e.g1, 1),
	} {
		if _, err := e.svc.ValidateWithoutCode(ctx, a, e.g1, r.id); err == nil {
			t.Fatalf("validation permise à %+v", a)
		}
	}
	if _, err := e.svc.ValidateWithoutCode(ctx, admin(e.g2), e.g2, r.id); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("inscription d'un autre groupe : %v", err)
	}
	e.draw.Done = map[int64]bool{e.g1: true}
	_, err := e.svc.ValidateWithoutCode(ctx, admin(e.g1), e.g1, r.id)
	code, _ := errs.CodeOf(err)
	if code != "registration.closed_draw" || !invite.ClosedDraw.AdjustDrawHint() {
		t.Fatalf("après le tirage : %v", err)
	}
	if len(e.participants(t, e.g1)) != 0 || len(e.welcome.all()) != 0 {
		t.Fatal("participant créé malgré le refus")
	}
}
