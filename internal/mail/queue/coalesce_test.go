package queue_test

import (
	"testing"
	"time"

	"secretsanta/internal/mail/queue"
)

func keyed(body string) queue.Item {
	it := item("lea@example.net", "Léa", 1)
	it.Type = "nouvelle_idee"
	it.Essential = false
	it.CoalesceKey = "idee:1"
	it.Text = "Bonjour Léa,\n\n" + body
	it.HTML = queue.HTMLFromText(it.Text)
	return it
}

func TestCoalesceWaitsTenMinutesAfterLastUpdate(t *testing.T) {
	e := newEnv(t, nil)
	id1 := mustEnqueue(t, e, keyed("une idée"))
	e.Clk.Advance(6 * time.Minute)
	id2 := mustEnqueue(t, e, keyed("deux idées"))
	if id1 != id2 {
		t.Fatal("même message attendu")
	}
	e.Clk.Advance(6 * time.Minute) // 12 min après la première, 6 après la dernière
	if tick(t, e) != 0 {
		t.Fatal("pas d'envoi avant 10 min après la dernière mise à jour")
	}
	e.Clk.Advance(4 * time.Minute)
	if tick(t, e) != 1 || e.Tr.count() != 1 {
		t.Fatalf("un seul envoi attendu, obtenu %d", e.Tr.count())
	}
	if !contains(e.Tr.Sent[0].Raw, "deux") {
		t.Fatal("le contenu doit être la dernière version")
	}
	if tick(t, e) != 0 {
		t.Fatal("pas de second envoi")
	}
}

func TestCoalesceNeverBeyondSixtyMinutes(t *testing.T) {
	e := newEnv(t, nil)
	id := mustEnqueue(t, e, keyed("v0"))
	for i := 0; i < 11; i++ { // une mise à jour toutes les 5 min
		e.Clk.Advance(5 * time.Minute)
		mustEnqueue(t, e, keyed("v"+string(rune('1'+i))))
		if tick(t, e) != 0 {
			t.Fatalf("envoi prématuré à +%d min", (i+1)*5)
		}
	}
	// +55 min : la mise à jour suivante est plafonnée à +60 min.
	e.Clk.Advance(5 * time.Minute)
	mustEnqueue(t, e, keyed("dernière"))
	st := status(t, e, id)
	if want := t0.Add(60 * time.Minute); !st.NextAttemptAt.Equal(want) {
		t.Fatalf("envoi prévu %v, attendu %v", st.NextAttemptAt, want)
	}
	if tick(t, e) != 1 || e.Tr.count() != 1 {
		t.Fatal("envoi attendu à +60 min au plus tard")
	}
}

func TestCoalesceCapSendsAtSixty(t *testing.T) {
	e := newEnv(t, nil)
	mustEnqueue(t, e, keyed("v0"))
	for i := 0; i < 12; i++ {
		e.Clk.Advance(5 * time.Minute)
		if i < 11 {
			mustEnqueue(t, e, keyed("x"))
		}
	}
	// +60 min : échéance plafonnée atteinte, un seul envoi.
	if tick(t, e) != 1 || e.Tr.count() != 1 {
		t.Fatalf("envoi attendu à +60 min, %d", e.Tr.count())
	}
}

func TestDifferentKeysAreIndependent(t *testing.T) {
	e := newEnv(t, nil)
	a := keyed("a")
	b := keyed("b")
	b.CoalesceKey = "idee:2"
	if mustEnqueue(t, e, a) == mustEnqueue(t, e, b) {
		t.Fatal("clés distinctes, messages distincts")
	}
	e.Clk.Advance(10 * time.Minute)
	if tick(t, e) != 2 {
		t.Fatal("deux envois attendus")
	}
	// Une fois parti, une nouvelle mise en file crée un nouveau message.
	if id := mustEnqueue(t, e, a); id == 0 {
		t.Fatal("nouveau message attendu")
	}
}
