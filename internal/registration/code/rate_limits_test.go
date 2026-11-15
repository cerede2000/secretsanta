package code

import (
	"context"
	"errors"
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/shared/errs"
)

// F11.AC11 : renvoi refusé avant 60 secondes, 4e code en une heure pour la
// même adresse et 6e inscription en 15 minutes depuis la même adresse IP
// refusés, puis réautorisés après la fenêtre, avec horloge simulée.

func TestResendRefusedBefore60Seconds(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	first := f.issue(t, testGroup, 1, "lea@example.org")

	f.clk.Advance(59 * time.Second)
	_, err := f.svc.Issue(ctx, request(testGroup, 1, "lea@example.org"))
	if !errors.Is(err, ErrTooSoon) || !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("renvoi à 59 s : %v", err)
	}
	if f.mailer.count() != 1 {
		t.Fatal("un renvoi refusé a envoyé un courriel")
	}
	// Le premier code reste valable.
	if _, ok, _ := f.store.Get(ctx, testGroup, 1); !ok {
		t.Fatal("le code a disparu")
	}

	f.clk.Advance(time.Second)
	iss, err := f.svc.Issue(ctx, request(testGroup, 1, "lea@example.org"))
	if err != nil {
		t.Fatalf("renvoi à 60 s : %v", err)
	}
	if !iss.ResendAt.Equal(f.clk.Now().Add(60*time.Second)) || !iss.ExpiresAt.Equal(f.clk.Now().Add(15*time.Minute)) {
		t.Fatalf("échéances %+v", iss)
	}
	second := f.mailer.last(t)
	// L'ancien code et l'ancien lien ne fonctionnent plus, le nouveau oui.
	if first.Code != second.Code {
		if _, err := f.svc.Verify(ctx, testGroup, 1, first.Code); !IsRefused(err) {
			t.Fatalf("ancien code accepté après renvoi : %v", err)
		}
	}
	if _, err := f.svc.Preview(ctx, testGroup, tokenOf(t, first)); !IsRefused(err) {
		t.Fatalf("ancien lien accepté après renvoi : %v", err)
	}
	if _, err := f.svc.Verify(ctx, testGroup, 1, second.Code); err != nil {
		t.Fatalf("nouveau code refusé : %v", err)
	}
}

func TestFourthCodePerAddressPerHourRefused(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const addr = "Partage@Example.org"
	// Trois codes pour la même adresse (adresse partagée par trois
	// inscriptions, casse différente : même seau), puis un renvoi.
	f.issue(t, testGroup, 1, addr)
	f.clk.Advance(10 * time.Minute)
	f.issue(t, testGroup, 2, "partage@example.org")
	f.clk.Advance(10 * time.Minute)
	f.issue(t, testGroup, 3, " partage@example.org ")
	f.clk.Advance(10 * time.Minute)

	_, err := f.svc.Issue(ctx, request(testGroup, 1, addr))
	if !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("4e code en une heure : %v", err)
	}
	if f.mailer.count() != 3 {
		t.Fatalf("%d courriels, 3 attendus", f.mailer.count())
	}
	// Une autre adresse n'est pas touchée.
	f.issue(t, testGroup, 4, "autre@example.org")
	// La même adresse dans un autre groupe a son propre seau.
	f.issue(t, testGroup+1, 1, addr)

	// La fenêtre d'une heure s'ouvre au premier code.
	f.clk.Advance(30*time.Minute - time.Second)
	if _, err := f.svc.Issue(ctx, request(testGroup, 1, addr)); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("juste avant la fin de la fenêtre : %v", err)
	}
	f.clk.Advance(time.Second)
	if _, err := f.svc.Issue(ctx, request(testGroup, 1, addr)); err != nil {
		t.Fatalf("après la fenêtre : %v", err)
	}
}

func TestTooSoonResendDoesNotSpendAddressQuota(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.issue(t, testGroup, 1, "lea@example.org")
	for i := 0; i < 5; i++ {
		if _, err := f.svc.Issue(ctx, request(testGroup, 1, "lea@example.org")); !errors.Is(err, ErrTooSoon) {
			t.Fatalf("renvoi immédiat : %v", err)
		}
	}
	f.clk.Advance(time.Minute)
	f.issue(t, testGroup, 1, "lea@example.org")
	f.clk.Advance(time.Minute)
	f.issue(t, testGroup, 1, "lea@example.org")
}

func TestSixthRegistrationPerIPIn15MinutesRefused(t *testing.T) {
	f := newFixture(t)
	const ip = "203.0.113.9"
	for i := 0; i < 5; i++ {
		if err := f.svc.AllowRegistration(testGroup, ip); err != nil {
			t.Fatalf("inscription %d : %v", i+1, err)
		}
		f.clk.Advance(time.Minute)
	}
	err := f.svc.AllowRegistration(testGroup, ip)
	if !errors.Is(err, limiter.ErrLimited) || !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("6e inscription : %v", err)
	}
	// Autre adresse IP, ou même adresse dans un autre groupe : permis.
	if err := f.svc.AllowRegistration(testGroup, "203.0.113.10"); err != nil {
		t.Fatalf("autre IP : %v", err)
	}
	if err := f.svc.AllowRegistration(testGroup+1, ip); err != nil {
		t.Fatalf("autre groupe : %v", err)
	}
	// La fenêtre de 15 minutes s'est ouverte à la première inscription.
	f.clk.Advance(10*time.Minute - time.Second)
	if err := f.svc.AllowRegistration(testGroup, ip); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("juste avant la fin de la fenêtre : %v", err)
	}
	f.clk.Advance(time.Second)
	if err := f.svc.AllowRegistration(testGroup, ip); err != nil {
		t.Fatalf("après la fenêtre : %v", err)
	}
}

func TestLimitRules(t *testing.T) {
	if CodesPerAddress != (limiter.Rule{Limit: 3, Window: time.Hour}) {
		t.Fatalf("plafond par adresse %+v", CodesPerAddress)
	}
	if RegistrationsPerIP != (limiter.Rule{Limit: 5, Window: 15 * time.Minute}) {
		t.Fatalf("plafond par IP %+v", RegistrationsPerIP)
	}
	if ResendDelay != time.Minute {
		t.Fatalf("délai de renvoi %v", ResendDelay)
	}
}
