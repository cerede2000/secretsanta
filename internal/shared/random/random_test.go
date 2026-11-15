package random

import (
	"encoding/base64"
	"testing"
)

func TestTokenIs128BitsAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 10000; i++ {
		tok, err := Token()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil || len(raw) != 16 {
			t.Fatalf("token %q: %d octets, err=%v", tok, len(raw), err)
		}
		if seen[tok] {
			t.Fatalf("doublon %q", tok)
		}
		seen[tok] = true
	}
}

func TestIntRange(t *testing.T) {
	if _, err := Int(0); err == nil {
		t.Fatal("attendu une erreur")
	}
	hit := [5]bool{}
	for i := 0; i < 500; i++ {
		v, err := Int(5)
		if err != nil || v < 0 || v >= 5 {
			t.Fatalf("v=%d err=%v", v, err)
		}
		hit[v] = true
	}
	for i, h := range hit {
		if !h {
			t.Fatalf("valeur %d jamais tirée", i)
		}
	}
}

func TestShuffle(t *testing.T) {
	a := []int{1, 2, 3, 4, 5, 6}
	if err := Shuffle(len(a), func(i, j int) { a[i], a[j] = a[j], a[i] }); err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, v := range a {
		sum += v
	}
	if sum != 21 {
		t.Fatal(a)
	}
}
