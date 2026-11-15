package websec

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ms, err := Migrations()
	if err != nil || len(ms) == 0 {
		t.Fatalf("migrations : %v (%d)", err, len(ms))
	}
	if err := db.Migrate(d, MigrationModule, ms); err != nil {
		t.Fatal(err)
	}
	return d
}

var t0 = time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC)

func newSessions(t *testing.T, pub string, opts SessionOptions) (*Sessions, *clock.Fake, *sql.DB) {
	t.Helper()
	d := openDB(t)
	clk := clock.NewFake(t0)
	return NewSessions(d, clk, mustPolicy(t, pub), opts), clk, d
}

// login crée la session et renvoie son cookie.
func login(t *testing.T, s *Sessions, id Identity, prev *http.Cookie) (*Session, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	if prev != nil {
		r.AddCookie(prev)
	}
	sess, err := s.Create(context.Background(), rec, r, id)
	if err != nil {
		t.Fatal(err)
	}
	cks := rec.Result().Cookies()
	if len(cks) != 1 {
		t.Fatalf("cookies : %+v", cks)
	}
	return sess, cks[0]
}

func reqWith(ck *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if ck != nil {
		r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	return r
}

func TestSessionLifecycleAndExpiryWithFakeClock(t *testing.T) {
	cases := []struct {
		id       Identity
		ttl      time.Duration
		sameSite http.SameSite
		name     string
	}{
		{Identity{Kind: KindAdmin, GroupID: "g1"}, DefaultAdminTTL, http.SameSiteStrictMode, "__Host-ss_admin"},
		{Identity{Kind: KindParticipant, GroupID: "g1", Subject: "p42"}, DefaultParticipantTTL, http.SameSiteLaxMode, "__Host-ss_participant"},
	}
	for _, c := range cases {
		t.Run(string(c.id.Kind), func(t *testing.T) {
			s, clk, _ := newSessions(t, "https://noel.example.org", SessionOptions{})
			ctx := context.Background()
			sess, ck := login(t, s, c.id, nil)
			if !sess.ExpiresAt.Equal(t0.Add(c.ttl)) {
				t.Fatalf("échéance %v", sess.ExpiresAt)
			}
			if ck.Name != c.name || !ck.HttpOnly || !ck.Secure || ck.SameSite != c.sameSite ||
				ck.Path != "/" || ck.Domain != "" || ck.MaxAge != int(c.ttl/time.Second) {
				t.Fatalf("cookie mal durci : %+v", ck)
			}
			got, err := s.Validate(ctx, reqWith(ck), c.id.Kind, "g1")
			if err != nil || got.Identity != c.id {
				t.Fatalf("validation : %+v %v", got, err)
			}
			clk.Advance(c.ttl - time.Nanosecond)
			if _, err := s.Validate(ctx, reqWith(ck), c.id.Kind, "g1"); err != nil {
				t.Fatalf("expirée trop tôt : %v", err)
			}
			clk.Advance(time.Nanosecond)
			if _, err := s.Validate(ctx, reqWith(ck), c.id.Kind, "g1"); !errors.Is(err, ErrNoSession) {
				t.Fatalf("valide à l'échéance : %v", err)
			}
			// Revenir en arrière ne ressuscite pas la session (supprimée).
			clk.Set(t0)
			if _, err := s.Validate(ctx, reqWith(ck), c.id.Kind, "g1"); !errors.Is(err, ErrNoSession) {
				t.Fatalf("session expirée ressuscitée : %v", err)
			}
		})
	}
}

func TestSessionOfOneGroupNeverValidForAnother(t *testing.T) {
	s, _, _ := newSessions(t, "http://localhost:8080", SessionOptions{})
	ctx := context.Background()
	_, adm := login(t, s, Identity{Kind: KindAdmin, GroupID: "g1"}, nil)
	_, par := login(t, s, Identity{Kind: KindParticipant, GroupID: "g1", Subject: "p1"}, nil)
	if adm.Secure || par.Secure || !adm.HttpOnly || !par.HttpOnly {
		t.Fatal("Secure doit suivre PUBLIC_URL (http) et HttpOnly toujours")
	}
	if adm.SameSite != http.SameSiteStrictMode || par.SameSite != http.SameSiteLaxMode {
		t.Fatal("SameSite")
	}
	for _, g := range []string{"g2", "", "G1", "g1 "} {
		if _, err := s.Validate(ctx, reqWith(adm), KindAdmin, g); !errors.Is(err, ErrNoSession) {
			t.Fatalf("admin g1 valide pour %q", g)
		}
		if _, err := s.Validate(ctx, reqWith(par), KindParticipant, g); !errors.Is(err, ErrNoSession) {
			t.Fatalf("participant g1 valide pour %q", g)
		}
	}
	// Un type n'en vaut pas un autre : la valeur d'un cookie participant
	// présentée comme cookie administrateur est refusée.
	forged := &http.Cookie{Name: s.CookieName(KindAdmin), Value: par.Value}
	if _, err := s.Validate(ctx, reqWith(forged), KindAdmin, "g1"); !errors.Is(err, ErrNoSession) {
		t.Fatal("cookie participant accepté comme administrateur")
	}
	// Via le middleware : les accesseurs exigent le groupe.
	var okG1, okG2, okP1, okP2 bool
	h := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, okG1 = AdminSession(r.Context(), "g1")
		_, okG2 = AdminSession(r.Context(), "g2")
		_, okP1 = ParticipantSession(r.Context(), "g1")
		_, okP2 = ParticipantSession(r.Context(), "g2")
	}))
	r := reqWith(adm)
	r.AddCookie(&http.Cookie{Name: par.Name, Value: par.Value})
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !okG1 || okG2 || !okP1 || okP2 {
		t.Fatalf("middleware : g1=%v g2=%v p1=%v p2=%v", okG1, okG2, okP1, okP2)
	}
}

func TestSessionInvalidCookiesAndDestroy(t *testing.T) {
	s, clk, d := newSessions(t, "https://noel.example.org", SessionOptions{AdminTTL: time.Hour, ParticipantTTL: 2 * time.Hour})
	ctx := context.Background()
	for _, v := range []string{"", "x", "AAAAAAAAAAAAAAAAAAAAAA"} {
		ck := &http.Cookie{Name: s.CookieName(KindAdmin), Value: v}
		if _, err := s.Validate(ctx, reqWith(ck), KindAdmin, "g1"); !errors.Is(err, ErrNoSession) {
			t.Fatalf("cookie %q accepté", v)
		}
	}
	if _, err := s.Validate(ctx, reqWith(nil), KindAdmin, "g1"); !errors.Is(err, ErrNoSession) {
		t.Fatal("sans cookie")
	}

	// L'identifiant n'est jamais stocké en clair.
	_, ck := login(t, s, Identity{Kind: KindAdmin, GroupID: "g1"}, nil)
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM websec_sessions WHERE id_hash = ?`, ck.Value).Scan(&n); err != nil || n != 0 {
		t.Fatalf("identifiant stocké en clair (%d, %v)", n, err)
	}

	// Nouvelle connexion : l'ancienne session est supprimée (pas de fixation).
	_, ck2 := login(t, s, Identity{Kind: KindAdmin, GroupID: "g2"}, &http.Cookie{Name: ck.Name, Value: ck.Value})
	if ck2.Value == ck.Value {
		t.Fatal("identifiant réutilisé")
	}
	if _, err := s.Validate(ctx, reqWith(ck), KindAdmin, "g1"); !errors.Is(err, ErrNoSession) {
		t.Fatal("ancienne session encore valide")
	}

	// Déconnexion.
	rec := httptest.NewRecorder()
	if err := s.Destroy(ctx, rec, reqWith(ck2), KindAdmin); err != nil {
		t.Fatal(err)
	}
	if cks := rec.Result().Cookies(); len(cks) != 1 || cks[0].MaxAge >= 0 || !cks[0].HttpOnly || cks[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie d'effacement : %+v", cks)
	}
	if _, err := s.Validate(ctx, reqWith(ck2), KindAdmin, "g2"); !errors.Is(err, ErrNoSession) {
		t.Fatal("session détruite encore valide")
	}

	// Identité incomplète refusée.
	if _, err := s.Create(ctx, httptest.NewRecorder(), nil, Identity{Kind: KindAdmin}); err == nil {
		t.Fatal("groupe manquant accepté")
	}
	if _, err := s.Create(ctx, httptest.NewRecorder(), nil, Identity{Kind: KindParticipant, GroupID: "g1"}); err == nil {
		t.Fatal("participant manquant accepté")
	}
	if _, err := s.Create(ctx, httptest.NewRecorder(), nil, Identity{Kind: "autre", GroupID: "g1"}); err == nil {
		t.Fatal("type inconnu accepté")
	}

	// Purge : seules les sessions échues disparaissent.
	login(t, s, Identity{Kind: KindAdmin, GroupID: "g1"}, nil)                     // 1 h
	login(t, s, Identity{Kind: KindParticipant, GroupID: "g1", Subject: "p"}, nil) // 2 h
	clk.Advance(time.Hour)
	if got, err := s.PurgeExpired(ctx); err != nil || got != 1 {
		t.Fatalf("purge : %d %v", got, err)
	}
}
