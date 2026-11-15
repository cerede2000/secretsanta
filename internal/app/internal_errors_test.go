package app

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Un 500 renvoyé au client est toujours journalisé, avec le motif de route et
// jamais le chemin réel (qui peut porter un jeton).
func TestServerErrorResponsesAreLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	a := &App{Mux: http.NewServeMux(), Slugs: http.NewServeMux()}
	a.Mux.HandleFunc("POST /creer", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Une erreur interne est survenue.", http.StatusInternalServerError)
	})
	a.Slugs.HandleFunc("GET /i/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	a.Mux.HandleFunc("GET /ok", func(http.ResponseWriter, *http.Request) {})
	h := a.logServerErrors(a.routes())

	for _, path := range []string{"/ok"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if buf.Len() != 0 {
		t.Fatalf("une réponse 200 a été journalisée : %q", buf.String())
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/creer", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/i/SECRET-TOKEN-VALUE", nil))
	out := buf.String()
	if !strings.Contains(out, "route=\"POST /creer\"") || !strings.Contains(out, "statut=500") {
		t.Errorf("500 sur /creer non journalisé : %q", out)
	}
	if !strings.Contains(out, "GET /i/{token}") {
		t.Errorf("500 sur la route à jeton non journalisé : %q", out)
	}
	if strings.Contains(out, "SECRET-TOKEN-VALUE") {
		t.Errorf("le jeton fuit dans le journal : %q", out)
	}
}
