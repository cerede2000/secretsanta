package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeDB struct{ err error }

func (f fakeDB) PingContext(context.Context) error { return f.err }

func TestHandlerOK(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(fakeDB{}, "0.1.2").ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var st Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.DB != "ok" || st.Version != "0.1.2" {
		t.Fatalf("%+v", st)
	}
}

func TestHandlerDBDown(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(fakeDB{errors.New("x")}, "v").ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 503 {
		t.Fatalf("code %d", rec.Code)
	}
	var st Status
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.DB != "unreachable" {
		t.Fatalf("%+v", st)
	}
}

func TestURL(t *testing.T) {
	for in, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/health",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/health",
		"[::]:9000":      "http://127.0.0.1:9000/health",
		"127.0.0.1:7000": "http://127.0.0.1:7000/health",
	} {
		if got := URL(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestCheckExitCodes(t *testing.T) {
	ok := httptest.NewServer(Handler(fakeDB{}, "v"))
	defer ok.Close()
	if err := Check(context.Background(), ok.URL, time.Second); err != nil {
		t.Fatal(err)
	}
	if Run(ok.Listener.Addr().String()) != 0 {
		t.Fatal("attendu 0")
	}
	bad := httptest.NewServer(Handler(fakeDB{errors.New("x")}, "v"))
	defer bad.Close()
	if Run(bad.Listener.Addr().String()) != 1 {
		t.Fatal("attendu 1 (503)")
	}
	down := httptest.NewServer(http.NotFoundHandler())
	addr := down.Listener.Addr().String()
	down.Close()
	if Run(addr) != 1 {
		t.Fatal("attendu 1 (arrêté)")
	}
}
