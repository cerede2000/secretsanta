package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"secretsanta/internal/config"
)

func clientCfg(t *testing.T, trusted, header string) config.ClientIP {
	t.Helper()
	p, none, err := config.ParseTrustedProxies(trusted)
	if err != nil {
		t.Fatal(err)
	}
	return config.ClientIP{Trusted: p, None: none, Header: header}
}

func TestClientIPConfigurable(t *testing.T) {
	cases := []struct {
		name, trusted, header string
		remote                string
		hdr                   map[string]string
		want                  string
	}{
		{"pair non fiable, XFF forgé ignoré", "", "", "198.51.100.7:1", map[string]string{"X-Forwarded-For": "6.6.6.6"}, "198.51.100.7"},
		{"pair fiable, chaîne XFF", "", "", "10.0.0.2:1", map[string]string{"X-Forwarded-For": "6.6.6.6, 203.0.113.9, 10.0.0.4"}, "203.0.113.9"},
		{"none ignore l'en-tête même du bouclage", "none", "", "127.0.0.1:1", map[string]string{"X-Forwarded-For": "203.0.113.9"}, "127.0.0.1"},
		{"CIDR perso : pair dedans", "198.51.100.0/24", "", "198.51.100.7:1", map[string]string{"X-Forwarded-For": "203.0.113.9"}, "203.0.113.9"},
		{"CIDR perso : le privé n'est plus fiable", "198.51.100.0/24", "", "10.0.0.2:1", map[string]string{"X-Forwarded-For": "203.0.113.9"}, "10.0.0.2"},
		{"CIDR perso : saut de confiance sauté", "198.51.100.0/24,192.0.2.1", "", "198.51.100.7:1", map[string]string{"X-Forwarded-For": "203.0.113.9, 192.0.2.1, 198.51.100.8"}, "203.0.113.9"},
		{"IP seule", "192.0.2.1", "", "192.0.2.1:1", map[string]string{"X-Forwarded-For": "203.0.113.9"}, "203.0.113.9"},
		{"X-Real-IP", "", "X-Real-IP", "127.0.0.1:1", map[string]string{"X-Real-IP": "203.0.113.9", "X-Forwarded-For": "6.6.6.6"}, "203.0.113.9"},
		{"X-Real-IP insensible à la casse du réglage", "", "x-real-ip", "127.0.0.1:1", map[string]string{"X-Real-IP": "203.0.113.9"}, "203.0.113.9"},
		{"X-Real-IP depuis pair non fiable", "", "X-Real-IP", "198.51.100.7:1", map[string]string{"X-Real-IP": "6.6.6.6"}, "198.51.100.7"},
		{"CF-Connecting-IP", "", "CF-Connecting-IP", "10.0.0.2:1", map[string]string{"CF-Connecting-IP": "2001:db8::5"}, "2001:db8::5"},
		{"en-tête unique absent", "", "CF-Connecting-IP", "10.0.0.2:1", nil, "10.0.0.2"},
		{"en-tête illisible", "", "X-Real-IP", "10.0.0.2:1", map[string]string{"X-Real-IP": "n'importe quoi"}, "10.0.0.2"},
		{"XFF illisible", "", "", "10.0.0.2:1", map[string]string{"X-Forwarded-For": "zzz"}, "10.0.0.2"},
	}
	for _, c := range cases {
		f, err := newClientIP(clientCfg(t, c.trusted, c.header))
		if err != nil {
			t.Fatalf("%s : %v", c.name, err)
		}
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		for k, v := range c.hdr {
			r.Header.Set(k, v)
		}
		if got := f(r); got != c.want {
			t.Errorf("%s : %q, attendu %q", c.name, got, c.want)
		}
	}
}

func TestClientIPInvalidConfig(t *testing.T) {
	if _, err := newClientIP(config.ClientIP{Header: "X-Evil"}); err == nil || !strings.Contains(err.Error(), "CLIENT_IP_HEADER") {
		t.Errorf("en-tête inconnu : erreur attendue, obtenu %v", err)
	}
	if _, _, err := config.ParseTrustedProxies("pas-une-ip"); err == nil {
		t.Error("CIDR invalide : erreur attendue")
	}
}
