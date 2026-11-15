package mailmsg

import (
	"errors"
	"strings"
	"testing"
)

// localMessage fabrique un message dont le lien est link.
func localMessage(link string) Message {
	m := validMessage()
	m.Text = strings.ReplaceAll(m.Text, testLink, link)
	m.HTML = strings.ReplaceAll(m.HTML, testLink, link)
	return m
}

func builderFor(t *testing.T, publicURL string) *Builder {
	t.Helper()
	return newBuilder(t, func(o *Options) { o.PublicURL = publicURL })
}

func TestHTTPLinkAcceptedOnLoopbackPublicURL(t *testing.T) {
	for _, base := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		b := builderFor(t, base)
		if _, err := b.Build(localMessage(base + "/famille/tirage")); err != nil {
			t.Errorf("%s : lien http local refusé : %v", base, err)
		}
	}
}

func TestHTTPLinkRefusedOnNonLocalPublicURL(t *testing.T) {
	b := builderFor(t, "http://noel.example.org")
	_, err := b.Build(localMessage("http://noel.example.org/famille/tirage"))
	if !errors.Is(err, ErrLink) {
		t.Fatalf("erreur %v, attendu ErrLink", err)
	}
	b = builderFor(t, "https://localhost")
	if _, err := b.Build(localMessage("http://localhost/x")); !errors.Is(err, ErrLink) {
		t.Fatalf("http refusé attendu quand PUBLIC_URL est en https : %v", err)
	}
}

func TestHTTPLinkToOtherHostOrPortRefusedOnLoopback(t *testing.T) {
	b := builderFor(t, "http://localhost:8080")
	for _, link := range []string{
		"http://localhost:9090/x",
		"http://localhost/x",
		"http://127.0.0.1:8080/x",
		"http://evil.example.com/x",
	} {
		if _, err := b.Build(localMessage(link)); !errors.Is(err, ErrLink) {
			t.Errorf("%s : erreur %v, attendu ErrLink", link, err)
		}
	}
}

func TestHTTPSStillRequiredElsewhere(t *testing.T) {
	p := Policy{PublicHost: "noel.example.org", AllowHTTP: false}
	if err := p.CheckLink("http://noel.example.org/x"); !errors.Is(err, ErrLink) {
		t.Fatalf("http accepté sans AllowHTTP : %v", err)
	}
}
