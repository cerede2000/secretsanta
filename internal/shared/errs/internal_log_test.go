package errs

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// Une erreur interne n'est jamais muette : sa cause part au journal.
func TestInternalLogsItsCause(t *testing.T) {
	buf := captureLog(t)
	_ = Internal(errors.New("database is locked (5) (SQLITE_BUSY)"))
	out := buf.String()
	if !strings.Contains(out, "erreur interne") || !strings.Contains(out, "SQLITE_BUSY") {
		t.Fatalf("cause absente du journal : %q", out)
	}
}

// Le journal ne reçoit ni adresse électronique, ni URL, ni jeton.
func TestInternalLogRedactsPersonalData(t *testing.T) {
	buf := captureLog(t)
	_ = Internal(errors.New("envoi à alice@example.org via https://x.example/i/AbCdEfGhIjKlMnOpQrStUvWxYz012345 : jeton AbCdEfGhIjKlMnOpQrStUvWxYz012345 refusé"))
	out := buf.String()
	for _, secret := range []string{"alice", "example.org", "AbCdEfGh", "/i/"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q fuit dans le journal : %q", secret, out)
		}
	}
}
