package verify

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/web"
)

// F11.AC14 : une inscription vérifiée par son code devient un participant
// créé par CreateVerified (joined_at = instant de la vérification, lien
// personnel) et reçoit le courriel « bienvenue ».
func TestVerifiedCodeCreatesParticipantThroughCreateVerified(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	if n := e.credentialRows(t); n != 0 {
		t.Fatalf("%d secrets personnels avant vérification", n)
	}

	e.clock.Advance(3 * time.Minute)
	verifiedAt := e.clock.Now()
	ok, err := e.svc.VerifyCode(ctx, e.g1, r.ref, strings.ToLower(e.code(t, r)))
	if err != nil {
		t.Fatalf("vérification : %v", err)
	}
	if ok.AlreadyVerified || ok.ParticipantID == 0 || ok.Slug != "noel" || !ok.VerifiedAt.Equal(verifiedAt) {
		t.Fatalf("réussite inattendue : %+v", ok)
	}

	ps := e.participants(t, e.g1)
	if len(ps) != 1 {
		t.Fatalf("%d participants, 1 attendu", len(ps))
	}
	p := ps[0]
	if p.ID != ok.ParticipantID || p.FirstName != "Léa" || p.Name != "Martin" || p.Email != "lea@example.org" {
		t.Fatalf("participant %+v", p)
	}
	if !p.JoinedAt.Equal(verifiedAt) {
		t.Fatalf("joined_at = %v, attendu l'instant de vérification %v", p.JoinedAt, verifiedAt)
	}

	// Lien et code personnels de F2b : deux secrets hachés pour ce participant.
	if n := e.credentialRows(t); n != 2 {
		t.Fatalf("%d secrets personnels, 2 attendus (lien et code)", n)
	}
	ws := e.welcome.all()
	if len(ws) != 1 {
		t.Fatalf("%d courriels de bienvenue", len(ws))
	}
	w := ws[0]
	if w.To != "lea@example.org" || w.FirstName != "Léa" || w.ParticipantID != p.ID || w.GroupID != e.g1 || w.GroupName != "Groupe noel" {
		t.Fatalf("bienvenue %+v", w)
	}
	prefix := "https://noel.example.org/noel/acces/"
	if !strings.HasPrefix(w.Link, prefix) {
		t.Fatalf("lien personnel %q", w.Link)
	}
	pid, err := e.tokens.ResolveLink(ctx, strconv.FormatInt(e.g1, 10), strings.TrimPrefix(w.Link, prefix))
	if err != nil || pid != strconv.FormatInt(p.ID, 10) {
		t.Fatalf("le lien de bienvenue ne mène pas au participant : %q %v", pid, err)
	}

	// L'inscription n'est plus en attente et le code ne sert plus.
	if n := e.pendingCount(t, e.g1); n != 0 {
		t.Fatalf("%d inscriptions en attente", n)
	}
	if _, err := e.svc.VerifyCode(ctx, e.g1, r.ref, e.code(t, r)); err == nil {
		t.Fatal("un code déjà utilisé a servi deux fois")
	}
	if len(e.participants(t, e.g1)) != 1 || len(e.welcome.all()) != 1 {
		t.Fatal("doublon après une seconde saisie")
	}
}

// Le lien de confirmation en un clic suit le même chemin, au POST.
func TestConfirmLinkCreatesParticipant(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Noé", "Bernard", "noe@example.org")
	tok := e.confirmToken(t, r)

	pv, err := e.svc.PreviewLink(ctx, e.g1, tok)
	if err != nil || pv.RegistrationID != r.id || pv.Code != e.code(t, r) {
		t.Fatalf("aperçu %+v %v", pv, err)
	}
	if len(e.participants(t, e.g1)) != 0 {
		t.Fatal("l'aperçu (GET) a créé un participant")
	}
	e.clock.Advance(time.Minute)
	ok, err := e.svc.ConfirmLink(ctx, e.g1, tok)
	if err != nil {
		t.Fatal(err)
	}
	ps := e.participants(t, e.g1)
	if len(ps) != 1 || ps[0].ID != ok.ParticipantID || !ps[0].JoinedAt.Equal(e.clock.Now()) {
		t.Fatalf("participants %+v", ps)
	}
	if len(e.welcome.all()) != 1 {
		t.Fatal("bienvenue absente")
	}
	if _, err := e.svc.ConfirmLink(ctx, e.g1, tok); err == nil {
		t.Fatal("le jeton de confirmation a servi deux fois")
	}
	if len(e.participants(t, e.g1)) != 1 {
		t.Fatal("doublon")
	}
}

// Les règles de nom sont celles de F3, appliquées par CreateVerified : un
// homonyme ajouté à la main entre-temps fait échouer la vérification sans
// créer de doublon.
func TestNameRuleAppliedByCreateVerified(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Chloé", "Durand", "chloe@example.org")
	if _, err := e.parts.Create(ctx, admin(e.g1), e.g1, participants.Input{FirstName: "CHLOE", Name: "durand"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.VerifyCode(ctx, e.g1, r.ref, e.code(t, r))
	if !errs.Is(err, errs.KindConflict) {
		t.Fatalf("conflit de nom attendu, obtenu %v", err)
	}
	if len(e.participants(t, e.g1)) != 1 || len(e.welcome.all()) != 0 {
		t.Fatal("un homonyme a été créé")
	}
}

type captureQueue struct{ items []queue.Item }

func (c *captureQueue) EnqueueMail(_ context.Context, it queue.Item) (int64, error) {
	c.items = append(c.items, it)
	return int64(len(c.items)), nil
}

// Le courriel part avec le modèle « bienvenue » de F6 : lien personnel et
// incitation à la lettre, essentiel.
func TestWelcomeUsesTemplate(t *testing.T) {
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	q := &captureQueue{}
	link := "https://noel.example.org/noel/acces/abcdefabcdefabcdefabcdefabcdefab"
	err = TemplateMailer{Catalog: cat, Queue: q}.SendWelcome(t.Context(), Welcome{
		GroupID: 3, ParticipantID: 9, To: "lea@example.org", FirstName: "Léa", GroupName: "Famille", Link: link,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.items) != 1 {
		t.Fatalf("%d courriels", len(q.items))
	}
	it := q.items[0]
	if it.To != "lea@example.org" || it.GroupID != 3 || it.ParticipantID != 9 || !it.Essential ||
		it.Type != WelcomeMailType || it.UnsubscribeURL != "" {
		t.Fatalf("courriel %+v", it)
	}
	for _, part := range []string{it.Text, it.HTML} {
		if !strings.Contains(part, link) {
			t.Errorf("lien personnel absent :\n%s", part)
		}
		if !strings.Contains(part, "écrire ta lettre") {
			t.Errorf("incitation à la lettre absente :\n%s", part)
		}
	}
}

// Aucune règle de nom n'est recopiée dans internal/registration : la clé
// d'unicité vient de participants.NormalizeName et le contrôle de
// participants.NameExists ou de CreateVerified.
func TestNoNameRuleDuplicatedInRegistration(t *testing.T) {
	root := ".."
	banned := map[string]bool{
		"golang.org/x/text/unicode/norm": true, "golang.org/x/text/runes": true,
		"golang.org/x/text/transform": true, "golang.org/x/text/cases": true,
	}
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		files++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); banned[p] {
				t.Errorf("%s importe %s (normalisation des noms recopiée)", path, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				// Les méthodes des ports (Directory.NameExists, qui délègue à
				// participants.NameExists) sont permises ; une fonction libre
				// qui calcule la règle ne l'est pas.
				if x.Recv != nil {
					return true
				}
				name := strings.ToLower(x.Name.Name)
				for _, bad := range []string{"normalizename", "nameexists", "namekey", "foldname", "stripaccent", "removeaccent"} {
					if strings.Contains(name, bad) {
						t.Errorf("%s définit %s : règle de nom recopiée", path, x.Name.Name)
					}
				}
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "unicode" && x.Sel.Name == "Mn" {
					t.Errorf("%s retire les accents lui-même (unicode.Mn)", path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 5 {
		t.Fatalf("seulement %d fichiers analysés : mauvaise racine", files)
	}
}

// Dès que le tirage est fait, une inscription en attente ne peut plus être
// vérifiée (renvoi vers « Ajuster le tirage ») et son code n'est pas brûlé.
func TestVerificationRefusedAfterDraw(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	r := e.register(t, e.g1, "Léa", "Martin", "lea@example.org")
	e.draw.Done = map[int64]bool{e.g1: true}
	for _, try := range []func() error{
		func() error { _, err := e.svc.VerifyCode(ctx, e.g1, r.ref, e.code(t, r)); return err },
		func() error { _, err := e.svc.ConfirmLink(ctx, e.g1, e.confirmToken(t, r)); return err },
	} {
		err := try()
		if c, _ := errs.CodeOf(err); c != "registration.closed_draw" {
			t.Fatalf("après le tirage : %v", err)
		}
	}
	if len(e.participants(t, e.g1)) != 0 || len(e.welcome.all()) != 0 {
		t.Fatal("participant créé après le tirage")
	}
}
