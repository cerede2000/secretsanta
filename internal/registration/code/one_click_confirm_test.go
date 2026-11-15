package code

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// F11.AC13 : le lien de confirmation en un clic n'a aucun effet en GET (il
// affiche la page de confirmation), la vérification se fait au POST et le
// jeton ne sert qu'une fois.

type confirmBench struct {
	f     *fixture
	mux   *http.ServeMux
	pages []ConfirmPageData
	done  []Result
}

func newConfirmBench(t *testing.T) *confirmBench {
	t.Helper()
	b := &confirmBench{f: newFixture(t)}
	h := ConfirmHandler{
		Service: b.f.svc,
		Group: func(r *http.Request) (int64, bool) {
			if r.PathValue("slug") == "famille7" {
				return testGroup, true
			}
			return 0, false
		},
		Page: func(w http.ResponseWriter, _ *http.Request, d ConfirmPageData) {
			b.pages = append(b.pages, d)
			fmt.Fprintf(w, "code=%s bouton=Confirmer mon inscription", d.Preview.Code)
		},
		Done: func(w http.ResponseWriter, _ *http.Request, res Result) {
			b.done = append(b.done, res)
			w.WriteHeader(http.StatusSeeOther)
		},
	}
	b.mux = http.NewServeMux()
	b.mux.Handle("GET /{slug}/inscription/confirmer/{token}", h.PageHandler())
	b.mux.Handle("POST /{slug}/inscription/confirmer/{token}", h.SubmitHandler())
	return b
}

func (b *confirmBench) do(method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	b.mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func pathOf(t *testing.T, m Mail) string {
	t.Helper()
	const host = "https://noel.example.org"
	if !strings.HasPrefix(m.Link, host) {
		t.Fatalf("lien %q", m.Link)
	}
	return strings.TrimPrefix(m.Link, host)
}

func TestConfirmGetHasNoEffect(t *testing.T) {
	b := newConfirmBench(t)
	m := b.f.issue(t, testGroup, 1, "lea@example.org")
	before := b.f.store.Records()

	// Un scanner précharge le lien plusieurs fois.
	for i := 0; i < 3; i++ {
		rec := b.do(http.MethodGet, pathOf(t, m))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET : %d %s", rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "code="+m.Code) {
			t.Fatalf("la page n'affiche pas le code déjà saisi : %s", rec.Body)
		}
		if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("en-têtes %v", rec.Header())
		}
	}
	if rec := b.do(http.MethodHead, pathOf(t, m)); rec.Code != http.StatusOK {
		t.Fatalf("HEAD : %d", rec.Code)
	}
	if !reflect.DeepEqual(before, b.f.store.Records()) {
		t.Fatalf("le GET a modifié l'état :\n%+v\n%+v", before, b.f.store.Records())
	}
	if len(b.done) != 0 {
		t.Fatal("le GET a vérifié l'inscription")
	}
	if b.pages[0].Preview.RegistrationID != 1 || b.pages[0].GroupID != testGroup {
		t.Fatalf("page %+v", b.pages[0])
	}
	// Après les GET, le code saisi à la main fonctionne toujours.
	if _, err := b.f.svc.Verify(context.Background(), testGroup, 1, m.Code); err != nil {
		t.Fatalf("code refusé après les GET : %v", err)
	}
}

func TestConfirmPostVerifiesOnceOnly(t *testing.T) {
	b := newConfirmBench(t)
	m := b.f.issue(t, testGroup, 1, "lea@example.org")
	path := pathOf(t, m)

	rec := b.do(http.MethodPost, path)
	if rec.Code != http.StatusSeeOther || len(b.done) != 1 {
		t.Fatalf("POST : %d, %d vérifications", rec.Code, len(b.done))
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("Referrer-Policy absent du POST")
	}
	res := b.done[0]
	if res.GroupID != testGroup || res.RegistrationID != 1 || !res.VerifiedAt.Equal(b.f.clk.Now()) {
		t.Fatalf("résultat %+v", res)
	}

	// Le jeton ne sert qu'une fois : ni second POST, ni page, ni code.
	if rec := b.do(http.MethodPost, path); rec.Code != http.StatusNotFound || len(b.done) != 1 {
		t.Fatalf("second POST : %d, %d vérifications", rec.Code, len(b.done))
	}
	if rec := b.do(http.MethodGet, path); rec.Code != http.StatusNotFound {
		t.Fatalf("GET après usage : %d", rec.Code)
	}
	if _, err := b.f.svc.Verify(context.Background(), testGroup, 1, m.Code); !IsRefused(err) {
		t.Fatalf("code accepté après confirmation par le lien : %v", err)
	}
}

func TestConfirmSubmitRefusesSafeMethodsAndOtherGroups(t *testing.T) {
	b := newConfirmBench(t)
	m := b.f.issue(t, testGroup, 1, "lea@example.org")
	sub := ConfirmHandler{Service: b.f.svc, Group: func(*http.Request) (int64, bool) { return testGroup, true },
		Done: func(http.ResponseWriter, *http.Request, Result) { t.Fatal("méthode sûre vérifiée") }}.SubmitHandler()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/x", nil)
		req.SetPathValue("token", tokenOf(t, m))
		sub.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s sur le POST : %d", method, rec.Code)
		}
	}
	// Le même jeton sous le slug d'un autre groupe : refus neutre.
	other := strings.Replace(pathOf(t, m), "/famille7/", "/autre/", 1)
	if rec := b.do(http.MethodPost, other); rec.Code != http.StatusNotFound {
		t.Fatalf("autre groupe : %d", rec.Code)
	}
	if rec := b.do(http.MethodPost, pathOf(t, m)); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST légitime : %d", rec.Code)
	}
}

// barrierStore retient chaque FindByToken jusqu'à ce que n lectures soient
// arrivées : toutes les confirmations concurrentes passent la lecture, seule
// la consommation atomique les départage.
type barrierStore struct {
	*MemoryStore
	wg *sync.WaitGroup
}

func (b barrierStore) FindByToken(ctx context.Context, groupID int64, h string) (Record, bool, error) {
	r, ok, err := b.MemoryStore.FindByToken(ctx, groupID, h)
	b.wg.Done()
	b.wg.Wait()
	return r, ok, err
}

func TestConfirmTokenSingleUseUnderConcurrency(t *testing.T) {
	f := newFixture(t)
	m := f.issue(t, testGroup, 1, "lea@example.org")
	tok := tokenOf(t, m)
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	svc, err := New(Options{Store: barrierStore{f.store, &wg}, Clock: f.clk, Mailer: f.mailer, Link: testLink, Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	okc := make(chan bool, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := f.svc.Confirm(context.Background(), testGroup, tok)
			okc <- err == nil
		}()
	}
	wins := 0
	for i := 0; i < n; i++ {
		if <-okc {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d confirmations réussies, 1 attendue", wins)
	}
}
