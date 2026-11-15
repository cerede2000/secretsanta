package groups

import (
	"context"
	"errors"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/errs"
)

// F2.AC1 : création refusée sans le bon mot de passe d'instance ;
// comparaison indépendante de la longueur du préfixe correct.

func TestCreateRefusedWithoutInstancePassword(t *testing.T) {
	e := newEnv(t)
	for i, pw := range []string{"", "faux", testPassword + "x", testPassword[:len(testPassword)-1], " " + testPassword} {
		in := validInput("famille")
		in.InstancePassword = pw
		in.ClientKey = "client-" + string(rune('a'+i))
		_, err := e.svc.Create(context.Background(), in)
		wantFrench(t, err, errs.KindUnauthorized)
	}
	if countGroups(t, e.db) != 0 {
		t.Fatal("un groupe a été créé sans le bon mot de passe")
	}
	mustCreate(t, e, validInput("famille"))
	if countGroups(t, e.db) != 1 {
		t.Fatal("création avec le bon mot de passe non enregistrée")
	}
}

func TestWrongPasswordRevealsNothingAboutFields(t *testing.T) {
	e := newEnv(t)
	in := validInput("admin") // slug réservé
	in.InstancePassword = "faux"
	_, err := e.svc.Create(context.Background(), in)
	if errs.UserMessage(err) != msgInstancePassword {
		t.Fatalf("le mot de passe doit être vérifié avant les champs : %v", err)
	}
}

func TestEmptyInstancePasswordRefusesEverything(t *testing.T) {
	d := openDB(t)
	s, err := NewService(d, newEnv(t).clock, config.NewSecret(""))
	if err != nil {
		t.Fatal(err)
	}
	in := validInput("famille")
	in.InstancePassword = ""
	_, err = s.Create(context.Background(), in)
	wantFrench(t, err, errs.KindUnauthorized)
}

// La primitive à temps constant reçoit toujours deux condensés de 32 octets,
// quelle que soit la longueur du préfixe correct ou de la saisie : la durée
// ne peut pas en dépendre (subtle.ConstantTimeCompare sort tôt seulement sur
// des longueurs différentes).
func TestPasswordComparisonIndependentOfCorrectPrefix(t *testing.T) {
	e := newEnv(t)
	type call struct{ a, b int }
	var calls []call
	orig := constantTimeEq
	constantTimeEq = func(a, b []byte) int {
		calls = append(calls, call{len(a), len(b)})
		return orig(a, b)
	}
	defer func() { constantTimeEq = orig }()

	candidates := []string{""}
	for i := 1; i < len(testPassword); i++ {
		candidates = append(candidates, testPassword[:i]) // préfixes corrects croissants
		candidates = append(candidates, testPassword[:i]+"Z")
	}
	candidates = append(candidates, testPassword+"suffixe", string(make([]byte, 5000)))
	for _, c := range candidates {
		calls = calls[:0]
		if e.svc.instancePasswordOK(c) {
			t.Fatalf("%q accepté", c)
		}
		if len(calls) != 1 || calls[0] != (call{32, 32}) {
			t.Fatalf("comparaison non constante pour une saisie de %d octets : %v", len(c), calls)
		}
	}
	calls = calls[:0]
	if !e.svc.instancePasswordOK(testPassword) || len(calls) != 1 || calls[0] != (call{32, 32}) {
		t.Fatalf("bon mot de passe : %v", calls)
	}
}

func TestPasswordAttemptsAreLimited(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bad := validInput("famille")
	bad.InstancePassword = "faux"
	for i := 0; i < MaxPasswordFailures; i++ {
		_, err := e.svc.Create(ctx, bad)
		wantFrench(t, err, errs.KindUnauthorized)
	}
	// Bloqué, même avec le bon mot de passe.
	_, err := e.svc.Create(ctx, validInput("famille"))
	wantFrench(t, err, errs.KindForbidden)
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("cause attendue ErrTooManyAttempts : %v", err)
	}
	// Un autre client n'est pas bloqué.
	other := validInput("autre-famille")
	other.ClientKey = "203.0.113.9"
	mustCreate(t, e, other)
	// Après la fenêtre, le client retrouve l'accès.
	e.clock.Advance(PasswordFailWindow + time.Second)
	mustCreate(t, e, validInput("famille"))
}
