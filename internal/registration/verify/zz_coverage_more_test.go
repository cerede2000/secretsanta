package verify

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/web"
)

type zzEnqueuer struct{ items []queue.Item }

func (q *zzEnqueuer) EnqueueMail(_ context.Context, it queue.Item) (int64, error) {
	q.items = append(q.items, it)
	return int64(len(q.items)), nil
}

func TestZZTemplateMailer(t *testing.T) {
	ctx := context.Background()
	if err := (TemplateMailer{}).SendWelcome(ctx, Welcome{}); err == nil {
		t.Error("mailer vide accepté")
	}
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	q := &zzEnqueuer{}
	m := TemplateMailer{Catalog: cat, Queue: q}
	w := Welcome{GroupID: 1, ParticipantID: 2, To: "ana@example.org", FirstName: "Ana", GroupName: "Famille",
		Link: "https://noel.example.org/noel/acces/abc", Code: "123456"}
	if err := m.SendWelcome(ctx, w); err != nil {
		t.Fatal(err)
	}
	if len(q.items) != 1 || !q.items[0].Essential || q.items[0].Type != WelcomeMailType {
		t.Fatalf("file : %+v", q.items)
	}
	var got Welcome
	f := WelcomeMailerFunc(func(_ context.Context, w Welcome) error { got = w; return nil })
	if err := f.SendWelcome(ctx, w); err != nil || got.To != w.To {
		t.Errorf("func : %v", err)
	}
}

func TestZZPreviewAndAdminViews(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.register(t, e.g1, "Ana", "Zed", "ana@example.org")
	tok := e.confirmToken(t, r)

	if _, err := e.svc.PreviewLink(ctx, e.g1, tok); err != nil {
		t.Fatalf("aperçu : %v", err)
	}
	if _, err := e.svc.PreviewLink(ctx, e.g1, "inconnu"); err == nil {
		t.Error("jeton inconnu accepté")
	}
	if _, err := e.svc.PreviewLink(ctx, e.g2, tok); err == nil {
		t.Error("jeton d'un autre groupe accepté")
	}
	for _, a := range []participants.Actor{{}, participants.ParticipantActor(e.g1, 1), admin(e.g2)} {
		if _, err := e.svc.ListPending(ctx, a, e.g1); err == nil {
			t.Errorf("ListPending accepté pour %v", a.Kind)
		}
		if _, err := e.svc.Audit(ctx, a, e.g1); err == nil {
			t.Errorf("Audit accepté pour %v", a.Kind)
		}
		if err := e.svc.DeleteRegistration(ctx, a, e.g1, r.id); err == nil {
			t.Errorf("DeleteRegistration accepté pour %v", a.Kind)
		}
	}
	if err := e.svc.DeleteRegistration(ctx, admin(e.g1), e.g1, 424242); err == nil {
		t.Error("suppression d'une inscription inconnue acceptée")
	}
	list, err := e.svc.ListPending(ctx, admin(e.g1), e.g1)
	if err != nil || len(list) != 1 {
		t.Fatalf("liste : %v %v", list, err)
	}
	if err := e.svc.DeleteRegistration(ctx, admin(e.g1), e.g1, r.id); err != nil {
		t.Fatal(err)
	}
	au, err := e.svc.Audit(ctx, admin(e.g1), e.g1)
	if err != nil || len(au) == 0 {
		t.Fatalf("journal : %v %v", au, err)
	}
	// Après suppression, l'aperçu est refusé.
	if _, err := e.svc.PreviewLink(ctx, e.g1, tok); err == nil {
		t.Error("aperçu après suppression accepté")
	}
}

func TestZZMemoryStoreOutcomes(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	for i, exp := range []time.Time{now.Add(-time.Hour), now.Add(time.Hour)} {
		if err := s.SaveOutcome(ctx, Outcome{GroupID: 1, RegistrationID: int64(i + 1), Ref: string(rune('a' + i)), ExpiresAt: exp}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteOutcomesExpired(ctx, 1, now)
	if err != nil || n != 1 {
		t.Fatalf("purge : %d %v", n, err)
	}
	if _, ok, _ := s.Outcome(ctx, 1, 2); !ok {
		t.Error("résultat non expiré supprimé")
	}
}
