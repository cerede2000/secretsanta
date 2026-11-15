package settingspages

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F10c.AC6 : le lien vers le diagnostic, sans importer le paquet de F6c.
func TestMailSectionLinksToDeliverabilityWithoutImportingF6c(t *testing.T) {
	e := newEnv(t)
	gid := e.group("courrier")
	ck := e.adminCookie(gid)
	body := e.get("/courrier/admin/reglages", "fr", ck).Body.String()
	if !strings.Contains(body, `href="/admin/courrier/delivrabilite"`) {
		t.Error("lien vers le diagnostic absent")
	}
	if !strings.Contains(body, "smtp.interne.lan") || strings.Contains(body, "mot-de-passe") {
		t.Error("l'état SMTP doit s'afficher sans secret")
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			if strings.Contains(im.Path.Value, "maildiag") {
				t.Errorf("%s importe %s", f, im.Path.Value)
			}
		}
	}
	_ = os.Getenv
}

func TestMailTestSendsThroughQueueAndRejectsBadAddress(t *testing.T) {
	e := newEnv(t)
	gid := e.group("test")
	ck := e.adminCookie(gid)
	page := "/test/admin/reglages"
	if rec := e.post(page+"/courrier/test", page, ck, map[string][]string{"test_to": {"pas une adresse"}}); rec.Code != 422 || e.mail.calls != 0 {
		t.Fatalf("adresse invalide : %d", rec.Code)
	}
	rec := e.post(page+"/courrier/test", page, ck, map[string][]string{"test_to": {"camille@exemple.fr"}})
	if rec.Code != 200 || e.mail.calls != 1 || e.mail.item.To != "camille@exemple.fr" || e.mail.item.GroupID != gid {
		t.Fatalf("envoi : %d %+v", rec.Code, e.mail.item)
	}
	if !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Error("le résultat doit être annoncé")
	}
}
