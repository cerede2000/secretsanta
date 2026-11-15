package code

import (
	"context"
	"testing"
	"time"
)

// Contrat du magasin (référence pour le magasin SQL de F11.6) : la
// consommation est atomique et refuse un code remplacé, déjà utilisé, expiré
// ou au-delà du nombre de saisies permis ; le comptage ne touche qu'au code
// courant ; tout est limité au groupe.
func TestMemoryStoreContract(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	now := testStart
	put := func(reg int64, hash string) {
		t.Helper()
		if err := s.Put(ctx, Record{GroupID: 1, RegistrationID: reg, CodeHash: hash, TokenHash: "t" + hash,
			IssuedAt: now, ExpiresAt: now.Add(TTL)}); err != nil {
			t.Fatal(err)
		}
	}
	put(1, "a")
	if _, ok, _ := s.FindByToken(ctx, 2, "ta"); ok {
		t.Fatal("jeton trouvé dans un autre groupe")
	}
	if _, ok, _ := s.Get(ctx, 2, 1); ok {
		t.Fatal("code trouvé dans un autre groupe")
	}
	if r, ok, _ := s.FindByToken(ctx, 1, "ta"); !ok || r.RegistrationID != 1 {
		t.Fatal("jeton introuvable")
	}
	if _, ok, _ := s.AddAttempt(ctx, 1, 1, "autre"); ok {
		t.Fatal("tentative comptée sur un code remplacé")
	}
	for i := 1; i <= 3; i++ {
		if n, ok, _ := s.AddAttempt(ctx, 1, 1, "a"); !ok || n != i {
			t.Fatalf("tentative %d : %d %v", i, n, ok)
		}
	}
	if ok, _ := s.Consume(ctx, 1, 1, "a", now, 2); ok {
		t.Fatal("consommé au-delà des saisies permises")
	}
	if ok, _ := s.Consume(ctx, 1, 1, "a", now.Add(TTL), 5); ok {
		t.Fatal("consommé à l'expiration")
	}
	if ok, _ := s.Consume(ctx, 1, 1, "autre", now, 5); ok {
		t.Fatal("consommé avec une autre empreinte")
	}
	if ok, _ := s.Consume(ctx, 1, 1, "a", now.Add(TTL-time.Nanosecond), 3); !ok {
		t.Fatal("consommation refusée")
	}
	if ok, _ := s.Consume(ctx, 1, 1, "a", now, 5); ok {
		t.Fatal("consommé deux fois")
	}
	// Put remplace : nouveau code, compteur à zéro, ancien jeton oublié.
	put(1, "b")
	if _, ok, _ := s.FindByToken(ctx, 1, "ta"); ok {
		t.Fatal("ancien jeton encore trouvé")
	}
	if r, _, _ := s.Get(ctx, 1, 1); r.Attempts != 0 || r.Consumed {
		t.Fatalf("remplacement %+v", r)
	}
	if err := s.Delete(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, 1, 1); ok {
		t.Fatal("code non supprimé")
	}
	if err := s.Delete(ctx, 1, 1); err != nil {
		t.Fatal("suppression d'un code absent")
	}
}

// Invalidate (suppression, purge, « ignorer ») rend code et lien inutilisables.
func TestInvalidateStopsCodeAndLink(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.issue(t, testGroup, 1, "lea@example.org")
	if err := f.svc.Invalidate(ctx, testGroup, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Verify(ctx, testGroup, 1, m.Code); !IsRefused(err) {
		t.Fatalf("code accepté après invalidation : %v", err)
	}
	if _, err := f.svc.Preview(ctx, testGroup, tokenOf(t, m)); !IsRefused(err) {
		t.Fatalf("lien accepté après invalidation : %v", err)
	}
}
