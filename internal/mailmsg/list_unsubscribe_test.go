package mailmsg

import (
	"strings"
	"testing"
)

// F6b.AC5 : List-Unsubscribe (https et mailto) et List-Unsubscribe-Post
// seulement sur un message non essentiel qui porte une adresse de
// désinscription, jamais sur un message essentiel.

const (
	unsubURL    = "https://noel.example.org/desinscription/jeton"
	unsubMailto = "mailto:desinscription@example.org?subject=desinscription"
)

func unsubMessage(essential bool, url, mailto string) Message {
	m := validMessage()
	m.Essential = essential
	m.UnsubscribeURL = url
	m.UnsubscribeMailto = mailto
	if url != "" {
		m.Text += "Ne plus recevoir ces rappels : " + url + "\n"
		m.HTML += `<p><a href="` + url + `">Ne plus recevoir ces rappels</a> : ` + url + `</p>`
	}
	return m
}

func TestListUnsubscribeOnNonEssential(t *testing.T) {
	out := build(t, newBuilder(t, nil), unsubMessage(false, unsubURL, unsubMailto))
	msg := parse(t, out.Raw)
	got := msg.Header["List-Unsubscribe"]
	want := "<" + unsubURL + ">, <" + unsubMailto + ">"
	if len(got) != 1 || got[0] != want {
		t.Errorf("List-Unsubscribe = %v, attendu %q", got, want)
	}
	if post := msg.Header["List-Unsubscribe-Post"]; len(post) != 1 || post[0] != "List-Unsubscribe=One-Click" {
		t.Errorf("List-Unsubscribe-Post = %v", post)
	}

	// Lien https seul : en-tête https seul, Post présent.
	msg = parse(t, build(t, newBuilder(t, nil), unsubMessage(false, unsubURL, "")).Raw)
	if got := msg.Header.Get("List-Unsubscribe"); got != "<"+unsubURL+">" {
		t.Errorf("List-Unsubscribe sans mailto = %q", got)
	}
	if msg.Header.Get("List-Unsubscribe-Post") == "" {
		t.Errorf("List-Unsubscribe-Post absent avec un lien https")
	}
}

func TestListUnsubscribeNeverOnEssential(t *testing.T) {
	for _, c := range []struct{ url, mailto string }{
		{unsubURL, unsubMailto},
		{unsubURL, ""},
		{"", ""},
	} {
		out := build(t, newBuilder(t, nil), unsubMessage(true, c.url, c.mailto))
		assertNoListUnsubscribe(t, out.Raw)
	}
}

func TestListUnsubscribeAbsentWithoutAddress(t *testing.T) {
	out := build(t, newBuilder(t, nil), unsubMessage(false, "", ""))
	assertNoListUnsubscribe(t, out.Raw)
}

func assertNoListUnsubscribe(t *testing.T, raw []byte) {
	t.Helper()
	msg := parse(t, raw)
	for _, k := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if _, ok := msg.Header[k]; ok {
			t.Errorf("%s présent", k)
		}
	}
	if strings.Contains(strings.ToLower(string(raw)), "list-unsubscribe") {
		t.Errorf("trace de List-Unsubscribe dans le message")
	}
}

func TestListUnsubscribeAddressesValidated(t *testing.T) {
	b := newBuilder(t, nil)
	for _, c := range []struct{ url, mailto string }{
		{"http://noel.example.org/desinscription", ""},
		{"https://bit.ly/abc", ""},
		{"https://user@noel.example.org/x", ""},
		{"https://noel.example.org/a>,<https://evil.example.com", ""},
		{"/desinscription", ""},
		{"", unsubMailto}, // mailto sans lien https
		{unsubURL, "desinscription@example.org"},
		{unsubURL, "mailto:"},
		{unsubURL, "mailto:a@example.org,b@example.org"},
		{unsubURL, "https://noel.example.org/x"},
	} {
		m := validMessage()
		m.Essential = false
		m.UnsubscribeURL, m.UnsubscribeMailto = c.url, c.mailto
		if _, err := b.Build(m); err == nil {
			t.Errorf("désinscription %q / %q acceptée", c.url, c.mailto)
		}
	}
}

func TestListUnsubscribeURLMayLiveOffPublicHost(t *testing.T) {
	off := "https://desabonnement.example.com/u/jeton"
	out := build(t, newBuilder(t, nil), unsubMessage(false, off, unsubMailto))
	if got := parse(t, out.Raw).Header.Get("List-Unsubscribe"); !strings.Contains(got, off) {
		t.Errorf("List-Unsubscribe = %q", got)
	}
}
