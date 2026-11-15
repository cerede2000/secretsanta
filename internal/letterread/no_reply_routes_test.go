package letterread

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// AC2 : aucune route d'écriture, de réponse ou de commentaire n'existe pour
// le tireur.
func TestNoReplyRoutes(t *testing.T) {
	e := newEnv(t)
	h := NewHandler(e.svc, e.groups)
	routes := h.Routes()
	if len(routes) == 0 {
		t.Fatal("aucune route")
	}
	mux := http.NewServeMux()
	for _, r := range routes {
		if r.Method != http.MethodGet {
			t.Fatalf("route %s %s : seule la lecture (GET) est permise", r.Method, r.Pattern)
		}
		low := strings.ToLower(r.Pattern)
		for _, bad := range []string{"repon", "répon", "reply", "comment", "question", "message", "fil", "ecrire", "écrire"} {
			if strings.Contains(low, bad) {
				t.Fatalf("route %s : %q", r.Pattern, bad)
			}
		}
		mux.Handle(r.Method+" "+r.Pattern, r.Handler)
	}

	// Toute méthode d'écriture est refusée, par le routeur comme par le
	// gestionnaire lui-même.
	path := "/lecture/lettre-recue/1/image/1"
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(m, path, strings.NewReader("reponse=merci"))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s : %d, 405 attendu", m, path, rec.Code)
		}
		for _, r := range routes {
			rec := httptest.NewRecorder()
			r.Handler.ServeHTTP(rec, httptest.NewRequest(m, path, strings.NewReader("reponse=merci")))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("gestionnaire %s en %s : %d, 405 attendu", r.Pattern, m, rec.Code)
			}
		}
	}

	// En lecture, sans session participant : réponse neutre 404.
	e.group(t, "lecture")
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(m, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s sans session : %d, 404 attendu", m, rec.Code)
		}
	}

	// Le service n'offre au tireur que la lecture : la liste des méthodes
	// exportées est figée, et aucune ne répond, ne commente ni n'écrit.
	allowed := []string{"AdminRead", "ArchiveTx", "Image", "Read", "Stamp"}
	var got []string
	typ := reflect.TypeOf(e.svc)
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(allowed, ",") {
		t.Fatalf("méthodes exportées %v, attendues %v (revue obligatoire)", got, allowed)
	}
	for _, n := range got {
		low := strings.ToLower(n)
		for _, bad := range []string{"reply", "answer", "comment", "question", "message", "thread", "write", "post", "respond"} {
			if strings.Contains(low, bad) {
				t.Fatalf("méthode %s", n)
			}
		}
	}
	// ArchiveTx n'est pas une route : il ne prend aucun acteur, seulement la
	// transaction de l'ajustement.
	m, _ := typ.MethodByName("ArchiveTx")
	for i := 1; i < m.Type.NumIn(); i++ {
		if strings.Contains(m.Type.In(i).String(), "Actor") {
			t.Fatal("ArchiveTx ne doit pas être exposé à un participant")
		}
	}

	// Le code du paquet ne déclare aucune méthode HTTP d'écriture.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, en := range entries {
		n := en.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			switch x := node.(type) {
			case *ast.SelectorExpr:
				switch x.Sel.Name {
				case "MethodPost", "MethodPut", "MethodPatch", "MethodDelete", "RequireUnsafeMethod":
					t.Fatalf("%s : %s", n, x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(x.Value)
				if err != nil {
					return true
				}
				for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
					if s == m || strings.HasPrefix(s, m+" /") {
						t.Fatalf("%s : route %q", n, s)
					}
				}
			}
			return true
		})
	}
}
