package outbound

import (
	"context"
	"io"
	"net/url"
	"testing"
)

func TestCoverageNewRequest(t *testing.T) {
	u, _ := url.Parse("https://exemple.test/x")
	r, err := NewRequest(context.Background(), "POST", u, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.URL.String() != u.String() {
		t.Fatalf("requête inattendue %v", r)
	}
	rc, err := r.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "abc" {
		t.Fatalf("corps %q", b)
	}
	r2, err := NewRequest(context.Background(), "GET", u, nil)
	if err != nil || r2.Body != nil || r2.GetBody != nil {
		t.Fatalf("GET sans corps : %v %v", r2, err)
	}
	if _, err := NewRequest(context.Background(), "BAD METHOD", u, nil); err == nil {
		t.Fatal("méthode invalide acceptée")
	}
}
