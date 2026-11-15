package mailmsg

import (
	"bytes"
	"testing"
	"time"

	"secretsanta/internal/shared/clock"
)

// F6b.AC8 : avec une horloge et un aléa injectés, la construction d'un même
// message est déterministe (Date, Message-ID et frontières identiques).

func deterministicBuild(t *testing.T, seed byte, at time.Time) (*Built, string) {
	t.Helper()
	b := newBuilder(t, func(o *Options) {
		o.Clock = clock.NewFake(at)
		o.Random = seeded(seed)
	})
	m := validMessage()
	m.Essential = false
	m.UnsubscribeURL = unsubURL
	m.UnsubscribeMailto = unsubMailto
	m.Text += "Ne plus recevoir : " + unsubURL + "\n"
	m.HTML += `<p><a href="` + unsubURL + `">Ne plus recevoir</a> : ` + unsubURL + `</p>`
	out := build(t, b, m)
	boundary, _ := rawParts(t, parse(t, out.Raw))
	return out, boundary
}

func TestDeterministicWithInjectedClockAndRandom(t *testing.T) {
	a, ba := deterministicBuild(t, 7, testNow)
	b, bb := deterministicBuild(t, 7, testNow)
	if !bytes.Equal(a.Raw, b.Raw) {
		t.Fatalf("deux constructions identiques diffèrent :\n%s\n----\n%s", a.Raw, b.Raw)
	}
	ha, hb := parse(t, a.Raw).Header, parse(t, b.Raw).Header
	if ha.Get("Date") != hb.Get("Date") || ha.Get("Message-ID") != hb.Get("Message-ID") || ba != bb {
		t.Errorf("Date, Message-ID ou frontière différents")
	}
	if a.MessageID != b.MessageID {
		t.Errorf("Built.MessageID différents")
	}
	if ba == "" {
		t.Errorf("frontière vide")
	}
}

func TestDeterministicInputsDriveTheOutput(t *testing.T) {
	a, ba := deterministicBuild(t, 7, testNow)

	c, bc := deterministicBuild(t, 8, testNow)
	if a.MessageID == c.MessageID || ba == bc {
		t.Errorf("un autre aléa donne le même Message-ID ou la même frontière")
	}
	if parse(t, a.Raw).Header.Get("Date") != parse(t, c.Raw).Header.Get("Date") {
		t.Errorf("la date ne dépend pas de l'aléa")
	}

	d, bd := deterministicBuild(t, 7, testNow.Add(time.Hour))
	if got := parse(t, d.Raw).Header.Get("Date"); got != "Tue, 01 Dec 2026 10:30:15 +0000" {
		t.Errorf("Date = %q, attendu l'heure de l'horloge injectée", got)
	}
	if a.MessageID != d.MessageID || ba != bd {
		t.Errorf("Message-ID ou frontière dépendent de l'horloge")
	}
}
