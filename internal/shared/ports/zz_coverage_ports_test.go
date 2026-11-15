package ports

import (
	"context"
	"errors"
	"testing"
)

func TestCoverageFakeEmailSender(t *testing.T) {
	f := &FakeEmailSender{}
	if err := f.Send(context.Background(), EmailMessage{To: "a@b.test"}); err != nil {
		t.Fatal(err)
	}
	if len(f.Sent) != 1 || f.Sent[0].To != "a@b.test" {
		t.Fatalf("%v", f.Sent)
	}
	f.Err = errors.New("boom")
	if err := f.Send(context.Background(), EmailMessage{To: "c@d.test"}); err == nil || len(f.Sent) != 1 {
		t.Fatal("l'erreur doit empêcher l'enregistrement")
	}
}

func TestCoverageMemoryEventBus(t *testing.T) {
	b := &MemoryEventBus{}
	var got []string
	b.Subscribe("x", func(_ context.Context, e Event) error { got = append(got, "1"); return nil })
	boom := errors.New("boom")
	b.Subscribe("x", func(_ context.Context, e Event) error { got = append(got, "2"); return boom })
	b.Subscribe("x", func(_ context.Context, e Event) error { got = append(got, "3"); return nil })
	if err := b.Publish(context.Background(), Event{Name: "x"}); !errors.Is(err, boom) {
		t.Fatalf("erreur attendue, reçu %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("arrêt à la première erreur attendu : %v", got)
	}
	if err := b.Publish(context.Background(), Event{Name: "autre"}); err != nil {
		t.Fatal(err)
	}
	if len(b.Emitted) != 2 {
		t.Fatalf("émis %d", len(b.Emitted))
	}
}
