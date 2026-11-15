package magic_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/access/magic"
	"secretsanta/internal/shared/errs"
)

// F2b.AC5 : même réponse et même statut que l'adresse existe ou non, et le
// courriel ne part que pour une adresse connue.
func TestMagicLinkRequestSameResponseKnownOrUnknown(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	known, kerr := f.svc.RequestLink(ctx, magic.Request{GroupID: 1, Slug: "noel", Email: "anne@example.org", IP: "203.0.113.1"})
	unknown, uerr := f.svc.RequestLink(ctx, magic.Request{GroupID: 1, Slug: "noel", Email: "personne@example.org", IP: "203.0.113.2"})
	otherGroup, oerr := f.svc.RequestLink(ctx, magic.Request{GroupID: 2, Slug: "autre", Email: "bob@example.org", IP: "203.0.113.3"})
	noGroup, nerr := f.svc.RequestLink(ctx, magic.Request{GroupID: 0, Slug: "inconnu", Email: "anne@example.org", IP: "203.0.113.4"})
	for _, err := range []error{kerr, uerr, oerr, nerr} {
		if err != nil {
			t.Fatalf("erreur inattendue : %v", err)
		}
	}
	for _, r := range []magic.Response{unknown, otherGroup, noGroup} {
		if r != known {
			t.Fatalf("réponses différentes : %+v / %+v", known, r)
		}
	}
	if known.Status != http.StatusOK || known != magic.UniformResponse {
		t.Fatalf("réponse uniforme attendue, %+v", known)
	}
	f.svc.Wait()

	sent := f.out.messages()
	if len(sent) != 1 {
		t.Fatalf("un seul courriel attendu (adresse connue), %d", len(sent))
	}
	m := sent[0]
	if m.To != "anne@example.org" || m.Type != magic.EmailType || !m.Essential {
		t.Fatalf("courriel inattendu : %+v", m)
	}
	tok := tokenFrom(t, m)
	if pid, err := f.svc.Consume(ctx, 1, tok); err != nil || pid != 11 {
		t.Fatalf("le lien reçu doit connecter le participant : pid=%d err=%v", pid, err)
	}
}

// F2b.AC5 : la limitation et la validation répondent pareil pour une adresse
// connue ou inconnue.
func TestMagicLinkRefusalsAreUniform(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	for _, email := range []string{"anne@example.org", "personne@example.org"} {
		var last error
		for i := 0; i < 6; i++ {
			_, last = f.svc.RequestLink(ctx, magic.Request{GroupID: 1, Email: email, IP: fmt.Sprintf("198.51.100.%d", i+1)})
			if i < 5 && last != nil {
				t.Fatalf("%s : demande %d refusée à tort : %v", email, i+1, last)
			}
		}
		if !errors.Is(last, limiter.ErrLimited) {
			t.Fatalf("%s : la 6e demande doit être limitée, obtenu %v", email, last)
		}
		if errs.UserMessage(last) != errs.UserMessage(limiter.Error()) {
			t.Fatalf("%s : message de limitation non uniforme", email)
		}
	}
	for _, bad := range []string{"", "   ", "pas-une-adresse", "Anne <anne@example.org>"} {
		_, err := f.svc.RequestLink(ctx, magic.Request{GroupID: 1, Email: bad, IP: "203.0.113.200"})
		if !errs.Is(err, errs.KindValidation) {
			t.Fatalf("%q : erreur de validation attendue, %v", bad, err)
		}
	}
}

// F2b.AC5 : un temps comparable. La branche « adresse connue » est rendue
// délibérément lente (annuaire et envoi de 40 ms) : la durée de la demande
// doit rester la même, car le travail qui dépend de l'existence se fait
// après la réponse.
func TestMagicLinkRequestComparableTiming(t *testing.T) {
	f := newFixture(t, &limiter.Rule{Limit: 1_000_000, Window: time.Hour})
	f.dir.delay = 40 * time.Millisecond
	f.out.delay = 40 * time.Millisecond
	ctx := context.Background()

	const rounds = 40
	var known, unknown []time.Duration
	for i := 0; i < rounds; i++ {
		for _, email := range []string{"anne@example.org", fmt.Sprintf("x%d@example.org", i)} {
			start := time.Now()
			r, err := f.svc.RequestLink(ctx, magic.Request{GroupID: 1, Slug: "noel", Email: email, IP: "203.0.113.9"})
			d := time.Since(start)
			if err != nil || r != magic.UniformResponse {
				t.Fatalf("réponse inattendue : %+v %v", r, err)
			}
			if email == "anne@example.org" {
				known = append(known, d)
			} else {
				unknown = append(unknown, d)
			}
		}
	}
	mk, mu := median(known), median(unknown)
	t.Logf("médianes : connue %v, inconnue %v", mk, mu)
	// La lenteur de 80 ms de la branche connue ne doit pas transparaître.
	if mk >= 20*time.Millisecond || mu >= 20*time.Millisecond {
		t.Fatalf("la demande doit répondre sans attendre le traitement : connue %v, inconnue %v", mk, mu)
	}
	diff := mk - mu
	if diff < 0 {
		diff = -diff
	}
	if diff > 5*time.Millisecond {
		t.Fatalf("durées non comparables : connue %v, inconnue %v", mk, mu)
	}
	f.svc.Wait()
	if n := len(f.out.messages()); n != rounds {
		t.Fatalf("%d courriels attendus pour l'adresse connue, %d", rounds, n)
	}
}

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}
