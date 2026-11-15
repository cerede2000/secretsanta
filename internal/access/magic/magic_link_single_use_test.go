package magic_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/access/magic"
	"secretsanta/internal/shared/errs"
)

func mustIssue(t *testing.T, f *fixture, group, pid int64) string {
	t.Helper()
	tok, err := f.svc.Issue(context.Background(), group, pid)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func assertInvalid(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s : le lien doit être refusé", what)
	}
	if !errs.Is(err, errs.KindUnauthorized) {
		t.Fatalf("%s : refus uniforme attendu, obtenu %v", what, err)
	}
	if got, want := errs.UserMessage(err), "Ce lien de connexion n'est plus valable. Demandez-en un nouveau."; got != want {
		t.Fatalf("%s : message %q, attendu %q", what, got, want)
	}
}

// F2b.AC4 : le lien ne s'utilise qu'une fois.
func TestMagicLinkSingleUseSequential(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	tok := mustIssue(t, f, 1, 11)
	pid, err := f.svc.Consume(ctx, 1, tok)
	if err != nil || pid != 11 {
		t.Fatalf("première utilisation : pid=%d err=%v", pid, err)
	}
	_, err = f.svc.Consume(ctx, 1, tok)
	assertInvalid(t, err, "deuxième utilisation")
}

// F2b.AC4 : de deux requêtes simultanées (et plus), une seule réussit.
func TestMagicLinkSingleUseConcurrent(t *testing.T) {
	f := newFixture(t, nil)
	for round := 0; round < 5; round++ {
		tok := mustIssue(t, f, 1, 11)
		const n = 16
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				pid, err := f.svc.Consume(context.Background(), 1, tok)
				if err == nil && pid != 11 {
					t.Errorf("participant inattendu %d", pid)
				}
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		ok := 0
		for err := range results {
			if err == nil {
				ok++
				continue
			}
			if !errs.Is(err, errs.KindUnauthorized) {
				t.Fatalf("échec non uniforme (verrou ?) : %v", err)
			}
		}
		if ok != 1 {
			t.Fatalf("tour %d : %d utilisations réussies, 1 attendue", round, ok)
		}
	}
}

// F2b.AC4 : deux requêtes exactement simultanées sur deux connexions.
func TestMagicLinkTwoSimultaneousRequests(t *testing.T) {
	f := newFixture(t, nil)
	f.db.SetMaxOpenConns(4)
	tok := mustIssue(t, f, 1, 12)
	var wg sync.WaitGroup
	errsOut := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errsOut[i] = f.svc.Consume(context.Background(), 1, tok)
		}()
	}
	close(start)
	wg.Wait()
	if (errsOut[0] == nil) == (errsOut[1] == nil) {
		t.Fatalf("une et une seule requête doit réussir : %v / %v", errsOut[0], errsOut[1])
	}
}

// F2b.AC4 : le lien expire 15 minutes après sa création.
func TestMagicLinkExpiresAfterFifteenMinutes(t *testing.T) {
	if magic.TTL != 15*time.Minute {
		t.Fatalf("durée de validité %v, 15 min attendues", magic.TTL)
	}
	f := newFixture(t, nil)
	ctx := context.Background()

	tok := mustIssue(t, f, 1, 11)
	f.clk.Advance(15*time.Minute - time.Nanosecond)
	if pid, err := f.svc.Consume(ctx, 1, tok); err != nil || pid != 11 {
		t.Fatalf("juste avant 15 min le lien est valable : pid=%d err=%v", pid, err)
	}

	tok = mustIssue(t, f, 1, 11)
	f.clk.Advance(15 * time.Minute)
	_, err := f.svc.Consume(ctx, 1, tok)
	assertInvalid(t, err, "à 15 min")

	tok = mustIssue(t, f, 1, 11)
	f.clk.Advance(2 * time.Hour)
	_, err = f.svc.Consume(ctx, 1, tok)
	assertInvalid(t, err, "bien après 15 min")
}

// Le lien d'un groupe ne vaut rien dans un autre et la tentative ne le brûle
// pas ; un jeton inconnu ou mal formé reçoit le même refus.
func TestMagicLinkGroupIsolationAndUnknownTokens(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	tok := mustIssue(t, f, 1, 11)
	_, err := f.svc.Consume(ctx, 2, tok)
	assertInvalid(t, err, "autre groupe")
	if pid, err := f.svc.Consume(ctx, 1, tok); err != nil || pid != 11 {
		t.Fatalf("le bon groupe garde son lien : pid=%d err=%v", pid, err)
	}
	for _, bad := range []string{"", "abc", "AAAAAAAAAAAAAAAAAAAAAA", tok + "x", "../../etc/passwd", "AAAAAAAAAAAAAAAAAAAAA="} {
		_, err := f.svc.Consume(ctx, 1, bad)
		assertInvalid(t, err, "jeton "+bad)
	}
}

// Un nouveau lien révoque le précédent du même participant.
func TestNewLinkRevokesPrevious(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	old := mustIssue(t, f, 1, 11)
	other := mustIssue(t, f, 1, 12)
	fresh := mustIssue(t, f, 1, 11)
	_, err := f.svc.Consume(ctx, 1, old)
	assertInvalid(t, err, "ancien lien")
	if pid, err := f.svc.Consume(ctx, 1, fresh); err != nil || pid != 11 {
		t.Fatalf("nouveau lien : pid=%d err=%v", pid, err)
	}
	if pid, err := f.svc.Consume(ctx, 1, other); err != nil || pid != 12 {
		t.Fatalf("le lien d'un autre participant reste valable : pid=%d err=%v", pid, err)
	}
}

// Seul le hachage du jeton est stocké.
func TestTokenStoredHashed(t *testing.T) {
	f := newFixture(t, nil)
	tok := mustIssue(t, f, 1, 11)
	if len(tok) != 22 {
		t.Fatalf("jeton de 128 bits en base64 URL attendu (22 caractères), %q", tok)
	}
	rows, err := f.db.Query(`SELECT token_hash FROM magic_links`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var h []byte
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(h, []byte(tok)) || len(h) != 32 {
			t.Fatalf("hachage SHA-256 attendu, stocké %q", h)
		}
		n++
	}
	if n != 1 {
		t.Fatalf("1 ligne attendue, %d", n)
	}
}

func TestPurgeExpiredRemovesExpiredAndUsed(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	used := mustIssue(t, f, 1, 11)
	if _, err := f.svc.Consume(ctx, 1, used); err != nil {
		t.Fatal(err)
	}
	mustIssue(t, f, 2, 21)
	f.clk.Advance(10 * time.Minute)
	live := mustIssue(t, f, 1, 12)
	f.clk.Advance(6 * time.Minute)
	n, err := f.svc.PurgeExpired(ctx)
	if err != nil || n != 2 {
		t.Fatalf("2 liens purgés attendus : n=%d err=%v", n, err)
	}
	if pid, err := f.svc.Consume(ctx, 1, live); err != nil || pid != 12 {
		t.Fatalf("le lien vivant survit à la purge : pid=%d err=%v", pid, err)
	}
}
