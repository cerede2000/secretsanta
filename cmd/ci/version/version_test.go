package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNext(t *testing.T) {
	cases := []struct {
		name string
		base string
		tags []string
		want string
	}{
		{"sans tag", "0.1", nil, "0.1.0"},
		{"deux tags", "0.1", []string{"v0.1.0", "v0.1.1"}, "0.1.2"},
		{"nouvelle ligne", "0.2", []string{"v0.1.0", "v0.1.1"}, "0.2.0"},
		{"anciens tags supprimes", "0.1", []string{"v0.1.7", "v0.1.9"}, "0.1.10"},
		{"tags etrangers ignores", "0.1", []string{"v0.10.3", "foo", "v0.1.x", "v0.1.0"}, "0.1.1"},
		{"retour a la ligne", "0.1\n", nil, "0.1.0"},
	}
	for _, c := range cases {
		got, err := Next(c.base, c.tags)
		if err != nil || got != c.want {
			t.Errorf("%s : got %q, %v ; want %q", c.name, got, err, c.want)
		}
	}
}

func TestNextInvalid(t *testing.T) {
	for _, b := range []string{"", "1", "0.1.0", "a.b"} {
		if _, err := Next(b, nil); err == nil {
			t.Errorf("VERSION %q devrait etre refusee", b)
		}
	}
}

func TestRun(t *testing.T) {
	f := filepath.Join(t.TempDir(), "VERSION")
	if err := os.WriteFile(f, []byte("0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{f}, strings.NewReader("v0.1.0\nv0.1.1\n"), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "0.1.2\n" {
		t.Fatalf("got %q", out.String())
	}
}
