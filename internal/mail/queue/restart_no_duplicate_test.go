package queue_test

import (
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
)

func TestRestartResumesWithoutDuplicates(t *testing.T) {
	d := openDB(t)
	clk := clock.NewFake(t0)
	a := newEnvOn(t, d, clk, nil)
	ids := []int64{
		mustEnqueue(t, a, item("a@example.net", "Ana", 1)),
		mustEnqueue(t, a, item("b@example.net", "Bob", 2)),
		mustEnqueue(t, a, item("c@example.net", "Cléo", 3)),
	}
	// Le premier message part, puis le processus « s'arrête ».
	a.Tr.Fn = nil
	if _, err := d.Exec(`UPDATE mail_queue SET send_after=? WHERE id<>?`, t0.Add(time.Hour).UnixNano(), ids[0]); err != nil {
		t.Fatal(err)
	}
	if tick(t, a) != 1 {
		t.Fatal("un envoi attendu avant l'arrêt")
	}
	if _, err := d.Exec(`UPDATE mail_queue SET send_after=? WHERE id<>?`, t0.UnixNano(), ids[0]); err != nil {
		t.Fatal(err)
	}
	// Un message resté verrouillé par le processus disparu.
	if _, err := d.Exec(`UPDATE mail_queue SET status='sending', lock_token='x', locked_at=? WHERE id=?`, t0.UnixNano(), ids[2]); err != nil {
		t.Fatal(err)
	}

	b := newEnvOn(t, d, clk, nil) // redémarrage : nouveau service, même base
	if tick(t, b) != 1 || b.Tr.count() != 1 {
		t.Fatalf("seul le message libre doit partir, verrou encore valide (envois %d)", b.Tr.count())
	}
	clk.Advance(11 * time.Minute)
	if tick(t, b) != 1 {
		t.Fatal("le verrou périmé doit être repris")
	}
	if tick(t, b) != 0 || tick(t, a) != 0 {
		t.Fatal("rien ne doit repartir")
	}
	seen := map[string]int{}
	for _, tr := range []*fakeTransport{a.Tr, b.Tr} {
		for _, env := range tr.Sent {
			seen[env.To]++
		}
	}
	if len(seen) != 3 {
		t.Fatalf("trois destinataires attendus : %v", seen)
	}
	for to, n := range seen {
		if n != 1 {
			t.Fatalf("%s reçu %d fois", to, n)
		}
	}
}

func TestTwoWorkersNeverSendTwice(t *testing.T) {
	d := openDB(t)
	clk := clock.NewFake(t0)
	a, b := newEnvOn(t, d, clk, nil), newEnvOn(t, d, clk, nil)
	for i := 0; i < 20; i++ {
		mustEnqueue(t, a, item("x"+string(rune('a'+i))+"@example.net", "Ana", int64(i+1)))
	}
	done := make(chan struct{}, 2)
	for _, e := range []*env{a, b} {
		go func(e *env) { _, _ = e.Svc.Tick(t.Context()); done <- struct{}{} }(e)
	}
	<-done
	<-done
	seen := map[string]int{}
	for _, tr := range []*fakeTransport{a.Tr, b.Tr} {
		for _, env := range tr.Sent {
			seen[env.To]++
		}
	}
	if len(seen) != 20 {
		t.Fatalf("20 destinataires attendus, %d", len(seen))
	}
	for to, n := range seen {
		if n != 1 {
			t.Fatalf("%s reçu %d fois", to, n)
		}
	}
}
