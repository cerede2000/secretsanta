// Package health fournit la route /health et la sonde utilisée par la
// sous-commande « secretsanta healthcheck » (l'image finale n'a pas de shell).
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"secretsanta/internal/shared/outbound"
)

// Pinger vérifie que la base est joignable (satisfait par *sql.DB).
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Status est le corps JSON de /health.
type Status struct {
	Status  string `json:"status"`
	DB      string `json:"db"`
	Version string `json:"version"`
}

// Handler répond 200 quand la base répond, 503 sinon.
func Handler(db Pinger, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		st := Status{Status: "ok", DB: "ok", Version: version}
		code := http.StatusOK
		if db == nil || db.PingContext(ctx) != nil {
			st.Status, st.DB = "degraded", "unreachable"
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(st)
	})
}

// URL construit l'adresse locale de /health depuis une adresse d'écoute
// (« :8080 », « 0.0.0.0:8080 », « [::]:8080 »...).
func URL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://127.0.0.1:8080/health"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(strings.Trim(host, "[]"), port) + "/health"
}

// Check appelle l'adresse donnée et renvoie nil seulement pour un 200.
func Check(ctx context.Context, url string, timeout time.Duration) error {
	client := outbound.New(outbound.Options{Timeout: timeout})
	u, err := neturl.Parse(url)
	if err != nil {
		return err
	}
	req := (&http.Request{Method: http.MethodGet, URL: u, Host: u.Host, Header: http.Header{}}).WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("état HTTP %d", resp.StatusCode)
	}
	return nil
}

// Run exécute la sous-commande healthcheck et renvoie le code de sortie.
func Run(listenAddr string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Check(ctx, URL(listenAddr), 4*time.Second); err != nil {
		return 1
	}
	return 0
}
