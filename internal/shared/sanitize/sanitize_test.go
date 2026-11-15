package sanitize

import (
	"strings"
	"testing"

	"secretsanta/internal/shared/errs"
)

func TestControlCharsRemoved(t *testing.T) {
	got, err := Line("Al\x00ice\x07​", 50, "Le nom")
	if err != nil || got != "Alice" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestSpacesNormalized(t *testing.T) {
	got, _ := Line("  Jean \t\n  Pierre  Dupont  ", 50, "Le nom")
	if got != "Jean Pierre Dupont" {
		t.Fatalf("%q", got)
	}
}

func TestUnicodeNFC(t *testing.T) {
	got, _ := Line("élève", 50, "Le nom")
	if got != "élève" {
		t.Fatalf("%q", got)
	}
}

func TestMaxLength(t *testing.T) {
	if _, err := Line(strings.Repeat("é", 11), 10, "Le nom"); err == nil || !errs.Is(err, errs.KindValidation) {
		t.Fatal("attendu un refus")
	}
	if _, err := Line(strings.Repeat("é", 10), 10, "Le nom"); err != nil {
		t.Fatal(err)
	}
	// la longueur se mesure après nettoyage
	if _, err := Line("  "+strings.Repeat("a", 10)+"   ", 10, "Le nom"); err != nil {
		t.Fatal(err)
	}
}

func TestRequired(t *testing.T) {
	if _, err := Required(" \x00 ", 10, "Le nom"); err == nil {
		t.Fatal("attendu un refus")
	}
}

func TestTextKeepsNewlines(t *testing.T) {
	got, _ := Text("a  b\r\n\r\n\r\n\r\nc\x00", 100, "Le mot")
	if got != "a b\n\nc" {
		t.Fatalf("%q", got)
	}
}
