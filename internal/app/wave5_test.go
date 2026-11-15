package app

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/groups"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
)

const (
	testInstancePass = "mot-de-passe-d-instance-test"
	testClientAddr   = "198.51.100.7:40000"
)

// newAppCfg construit une application sur une base neuve ; mod ajuste la
// configuration.
func newAppCfg(t *testing.T, mod func(*config.Config)) *App {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := config.Config{
		PublicURL:    "http://localhost:8080",
		DataDir:      t.TempDir(),
		DefaultLang:  "fr",
		InstancePass: config.NewSecret(testInstancePass),
		SigningKey:   config.NewSecret("secret-de-signature-de-test"),
	}
	if mod != nil {
		mod(&cfg)
	}
	a, err := New(cfg, d, clock.NewFake(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

// browser est un client minimal : il garde les cookies et refuse toute
// réponse 500.
type browser struct {
	t       *testing.T
	a       *App
	cookies map[string]*http.Cookie
	addr    string
	headers map[string]string
}

func newBrowser(t *testing.T, a *App) *browser {
	return &browser{t: t, a: a, cookies: map[string]*http.Cookie{}, addr: testClientAddr, headers: map[string]string{}}
}

func (b *browser) do(method, target string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.RemoteAddr = b.addr
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range b.headers {
		req.Header.Set(k, v)
	}
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.a.Handler().ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
			continue
		}
		b.cookies[c.Name] = c
	}
	if rec.Code >= 500 {
		b.t.Errorf("%s %s : statut %d\n%s", method, target, rec.Code, rec.Body.String())
	}
	return rec
}

func (b *browser) get(target string) *httptest.ResponseRecorder {
	return b.do(http.MethodGet, target, nil)
}

// post ouvre d'abord la page `from` (cookie et jeton CSRF) puis soumet le
// formulaire à target avec ce jeton.
func (b *browser) post(from, target string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	page := b.get(from)
	m := csrfRe.FindStringSubmatch(page.Body.String())
	if m == nil {
		b.t.Fatalf("jeton CSRF absent de %s (statut %d) : %s", from, page.Code, page.Body.String())
	}
	f := url.Values{}
	for k, v := range form {
		f[k] = v
	}
	f.Set("csrf_token", html.UnescapeString(m[1]))
	return b.do(http.MethodPost, target, f)
}

var secretRe = regexp.MustCompile(`<code class="groups-secret" id="admin-secret">([^<]+)</code>`)

// createGroup crée un groupe par les pages et renvoie le secret affiché.
func createGroup(t *testing.T, b *browser, slug, name string) string {
	t.Helper()
	rec := b.post("/creer", "/creer", url.Values{
		"password": {testInstancePass}, "name": {name}, "slug": {slug}, "budget": {""},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("création : statut %d\n%s", rec.Code, rec.Body.String())
	}
	m := secretRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("secret absent de la page de création : %s", rec.Body.String())
	}
	return html.UnescapeString(m[1])
}

func TestCreatePageServed(t *testing.T) {
	rec := newBrowser(t, newApp(t)).get("/creer")
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `method="post"`) || csrfRe.FindString(body) == "" {
		t.Errorf("formulaire ou jeton CSRF absent : %s", body)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCreateWithoutCSRFRefused(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	rec := b.do(http.MethodPost, "/creer", url.Values{"password": {testInstancePass}, "name": {"Famille"}, "slug": {"famille"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("statut %d, attendu 403", rec.Code)
	}
	if _, err := a.Groups.BySlug(context.Background(), "famille"); err == nil {
		t.Error("le groupe a été créé sans jeton CSRF")
	}
}

func TestCreateWrongInstancePasswordShowsErrorPage(t *testing.T) {
	a := newApp(t)
	rec := newBrowser(t, a).post("/creer", "/creer", url.Values{
		"password": {"pas-le-bon"}, "name": {"Famille"}, "slug": {"famille"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("statut %d, attendu 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), html.EscapeString(a.Cat.T("fr", "groups.create.error.password", nil))) {
		t.Errorf("message d'erreur absent : %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "pas-le-bon") {
		t.Error("le mot de passe saisi est ré-affiché")
	}
	if _, err := a.Groups.BySlug(context.Background(), "famille"); err == nil {
		t.Error("groupe créé malgré un mauvais mot de passe")
	}
}

func TestCreateShowsSecretOnce(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	rec := b.post("/creer", "/creer", url.Values{
		"password": {testInstancePass}, "name": {"Noël en famille"}, "slug": {"noel"}, "budget": {"25"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
	m := secretRe.FindStringSubmatch(rec.Body.String())
	if m == nil || len(m[1]) < 16 {
		t.Fatalf("secret absent ou trop court : %v", m)
	}
	secret := html.UnescapeString(m[1])
	if !strings.Contains(rec.Body.String(), "http://localhost:8080/noel") {
		t.Errorf("adresse du groupe absente")
	}
	// Une seule fois : ni l'accueil, ni la connexion, ni un nouveau GET /creer.
	for _, p := range []string{"/noel", "/noel/connexion", "/noel/admin/connexion", "/creer"} {
		if r := b.get(p); strings.Contains(r.Body.String(), secret) {
			t.Errorf("le secret réapparaît sur %s", p)
		}
	}
	// Le même groupe ne se recrée pas.
	again := b.post("/creer", "/creer", url.Values{
		"password": {testInstancePass}, "name": {"Autre"}, "slug": {"noel"},
	})
	if again.Code != http.StatusConflict || strings.Contains(again.Body.String(), secret) {
		t.Errorf("double création : statut %d", again.Code)
	}
}

func TestGroupHomeShowsNoParticipantName(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	g, err := a.Groups.BySlug(context.Background(), "famille")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Participants.Create(context.Background(), participants.AdminActor(g.ID), g.ID,
		participants.Input{FirstName: "Zébulon", Name: "Quenotte", Email: "z@example.org"}); err != nil {
		t.Fatal(err)
	}
	rec := b.get("/famille")
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Famille Martin") {
		t.Errorf("nom du groupe absent : %s", body)
	}
	for _, leak := range []string{"Zébulon", "Quenotte", "z@example.org"} {
		if strings.Contains(body, leak) {
			t.Errorf("l'accueil public montre %q", leak)
		}
	}
}

func TestUnknownSlugNeutral404(t *testing.T) {
	b := newBrowser(t, newApp(t))
	a1, a2 := b.get("/groupe-inconnu-un"), b.get("/groupe-inconnu-deux")
	if a1.Code != http.StatusNotFound || a2.Code != http.StatusNotFound {
		t.Fatalf("statuts %d %d, attendu 404", a1.Code, a2.Code)
	}
	// Seul le champ « next » du sélecteur de langue reprend le chemin demandé.
	norm := func(rec *httptest.ResponseRecorder, slug string) string {
		s := strings.ReplaceAll(rec.Body.String(), slug, "SLUG")
		return csrfRe.ReplaceAllString(s, `name="csrf_token" value="X"`)
	}
	if norm(a1, "groupe-inconnu-un") != norm(a2, "groupe-inconnu-deux") {
		t.Error("la page 404 dépend du slug demandé")
	}
	if strings.Contains(a1.Body.String(), "Famille") || strings.Contains(a1.Body.String(), "existe") {
		t.Error("la page 404 n'est pas neutre")
	}
}

func TestLoginPageServed(t *testing.T) {
	b := newBrowser(t, newApp(t))
	createGroup(t, b, "famille", "Famille Martin")
	rec := b.get("/famille/connexion")
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Famille Martin") {
		t.Errorf("nom du groupe absent")
	}
	if b.get("/inconnu/connexion").Code != http.StatusNotFound {
		t.Error("connexion d'un groupe inconnu : 404 attendu")
	}
}

func TestMagicLinkConfirmationConsumesNothing(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")
	p, err := a.Participants.Create(context.Background(), participants.AdminActor(g.ID), g.ID,
		participants.Input{FirstName: "Léa", Name: "Martin", Email: "lea@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := a.Magic.Issue(context.Background(), g.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	link := "/famille/connexion/lien/" + tok
	for i := 0; i < 3; i++ {
		rec := b.get(link)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %d : statut %d", i, rec.Code)
		}
		body := rec.Body.String()
		if n := strings.Count(body, `action="`+link+`"`); n != 1 {
			t.Errorf("formulaire de confirmation : %d, attendu 1", n)
		}
		start := strings.Index(body, `action="`+link+`"`)
		end := strings.Index(body[start:], "</form>")
		if n := strings.Count(body[start:start+end], `<button`); n != 1 {
			t.Errorf("boutons du formulaire de confirmation : %d, attendu 1", n)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == a.Sessions.CookieName("participant") {
				t.Error("le GET ouvre une session")
			}
		}
	}
	// Le jeton est toujours valable : le POST ouvre la session.
	rec := b.post(link, link, url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/famille/participant" {
		t.Fatalf("consommation : statut %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// Usage unique.
	if again := b.post(link, link, url.Values{}); again.Code != http.StatusUnauthorized {
		t.Errorf("second usage : statut %d, attendu 401", again.Code)
	}
}

func TestAdminLoginWithCreationSecret(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	secret := createGroup(t, b, "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")

	rec := b.post("/famille/admin/connexion", "/famille/admin/connexion", url.Values{"secret": {secret}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/famille/admin" {
		t.Fatalf("connexion : statut %d, Location %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	name := a.Sessions.CookieName("admin")
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly {
		t.Fatalf("cookie de session administrateur absent ou lisible par script : %+v", sessionCookie)
	}
	// La session est reconnue pour ce groupe par la chaîne de middlewares.
	var ok bool
	a.Mux.HandleFunc("GET /_probe_session", func(w http.ResponseWriter, r *http.Request) {
		_, ok = websec.AdminSession(r.Context(), strconv.FormatInt(g.ID, 10))
	})
	req := httptest.NewRequest(http.MethodGet, "/_probe_session", nil)
	req.AddCookie(sessionCookie)
	a.Handler().ServeHTTP(httptest.NewRecorder(), req)
	if !ok {
		t.Error("la session administrateur n'est pas reconnue pour le groupe")
	}
}

// Un mauvais secret donne la même réponse qu'un groupe inconnu.
func TestAdminLoginWrongSecretSameAsUnknownGroup(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	attempt := func(slug, addr string) *httptest.ResponseRecorder {
		// Un navigateur neuf à chaque essai : cookies vierges.
		nb := newBrowser(t, a)
		nb.addr = addr
		return nb.post("/famille/admin/connexion", "/"+slug+"/admin/connexion", url.Values{"secret": {"pas-le-bon-secret"}})
	}
	wrong, unknown := attempt("famille", "203.0.113.1:1234"), attempt("inconnu", "203.0.113.2:1234")
	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("statuts %d et %d, attendu 401 des deux côtés", wrong.Code, unknown.Code)
	}
	norm := func(rec *httptest.ResponseRecorder, slug string) string {
		s := strings.ReplaceAll(rec.Body.String(), slug, "SLUG")
		return csrfRe.ReplaceAllString(s, `name="csrf_token" value="X"`)
	}
	if norm(wrong, "famille") != norm(unknown, "inconnu") {
		t.Error("la réponse d'un mauvais secret diffère de celle d'un groupe inconnu")
	}
	if strings.Contains(wrong.Body.String(), "Famille Martin") {
		t.Error("le nom du groupe fuit dans la réponse d'un mauvais secret")
	}
	for _, c := range wrong.Result().Cookies() {
		if c.Name == a.Sessions.CookieName("admin") {
			t.Error("session ouverte malgré un mauvais secret")
		}
	}
}

func TestFixedRoutesStillWork(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	if rec := b.get("/health"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "test") {
		t.Errorf("/health : %d %q", rec.Code, rec.Body.String())
	}
	if rec := b.get("/static/css/base/base.css"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("/static : %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := b.get("/sw.js"); rec.Code != http.StatusOK || rec.Header().Get("Service-Worker-Allowed") != "/" {
		t.Errorf("/sw.js : %d", rec.Code)
	}
	if rec := b.get("/images/inconnu.jpg"); rec.Code != http.StatusNotFound {
		t.Errorf("/images : %d", rec.Code)
	}
	if rec := b.get("/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), a.Cat.T("fr", "layout.home.title", nil)) {
		t.Errorf("/ : %d", rec.Code)
	}
	rec := b.post("/", "/langue", url.Values{"lang": {"en"}, "next": {"/creer"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/creer" {
		t.Errorf("POST /langue : %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Aucune route par groupe ne masque une route à chemin fixe : Mux d'abord.
func TestSlugRoutesDoNotMaskFixedRoutes(t *testing.T) {
	a := newApp(t)
	for _, p := range []string{"/health", "/static/", "/static/css/base/base.css", "/images/x.jpg", "/sw.js", "/", "/static/connexion", "/images/acces/jeton"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		if _, pattern := a.Mux.Handler(req); pattern == "" {
			t.Errorf("GET %s n'est pas retenu par les routes à chemin fixe", p)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/langue", nil)
	if _, pattern := a.Mux.Handler(req); pattern == "" {
		t.Error("POST /langue n'est pas retenu par les routes à chemin fixe")
	}
	// Les premiers segments fixes sont des slugs réservés : aucun groupe ne
	// peut les prendre (« langue » : POST seulement, sans conflit avec la page
	// d'un groupe, qui est en GET).
	for _, s := range []string{"health", "static", "images", "creer", "sw"} {
		if !groups.IsReservedSlug(s) {
			t.Errorf("slug %q non réservé", s)
		}
	}
}

func TestCreatePageInEnglish(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	b.headers["Cookie"] = "lang=en"
	rec := b.get("/creer")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `<html lang="en">`) {
		t.Fatalf("statut %d, langue : %.200s", rec.Code, body)
	}
	if want := a.Cat.T("en", "groups.create.title", nil); !strings.Contains(body, html.EscapeString(want)) {
		t.Errorf("titre anglais %q absent", want)
	}
	if fr := a.Cat.T("fr", "groups.create.title", nil); strings.Contains(body, html.EscapeString(fr)) {
		t.Errorf("titre français %q présent dans la page anglaise", fr)
	}
}

// La langue par défaut du groupe sert quand ni cookie ni navigateur ne
// tranchent.
func TestGroupDefaultLanguageApplies(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")
	if err := a.GroupSettings.SetDefaultLanguage(context.Background(), g.ID, "en"); err != nil {
		t.Fatal(err)
	}
	if body := b.get("/famille").Body.String(); !strings.Contains(body, `<html lang="en">`) {
		t.Errorf("langue du groupe non appliquée : %.200s", body)
	}
	b.headers["Cookie"] = "lang=fr"
	delete(b.cookies, "lang")
	if body := b.get("/famille").Body.String(); !strings.Contains(body, `<html lang="fr">`) {
		t.Errorf("le cookie doit primer sur la langue du groupe : %.200s", body)
	}
}

// Le changement de langue d'un participant connecté est enregistré.
func TestLanguageChoiceRememberedForConnectedParticipant(t *testing.T) {
	a := newApp(t)
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")
	p, err := a.Participants.Create(context.Background(), participants.AdminActor(g.ID), g.ID,
		participants.Input{FirstName: "Léa", Name: "Martin", Email: "lea@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := a.Magic.Issue(context.Background(), g.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	link := "/famille/connexion/lien/" + tok
	if rec := b.post(link, link, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("connexion : %d", rec.Code)
	}
	// Sans session de ce groupe dans « next », rien n'est enregistré.
	if rec := b.post("/", "/langue", url.Values{"lang": {"en"}, "next": {"/autre"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("langue : %d", rec.Code)
	}
	if got, _ := a.Participants.Get(context.Background(), participants.AdminActor(g.ID), g.ID, p.ID); got.Locale != "" {
		t.Errorf("langue enregistrée hors du groupe : %q", got.Locale)
	}
	if rec := b.post("/famille/connexion", "/langue", url.Values{"lang": {"en"}, "next": {"/famille/connexion"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("langue : %d", rec.Code)
	}
	got, err := a.Participants.Get(context.Background(), participants.AdminActor(g.ID), g.ID, p.ID)
	if err != nil || got.Locale != "en" {
		t.Errorf("langue du participant %q (%v), attendu en", got.Locale, err)
	}
}

// fakeTransport enregistre les enveloppes envoyées.
type fakeTransport struct {
	mu   sync.Mutex
	sent []queue.Envelope
}

func (f *fakeTransport) Send(_ context.Context, e queue.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, e)
	return nil
}

// Sans transport, la file ne tourne pas : la demande de lien magique reçoit la
// réponse uniforme et l'envoi est refusé proprement.
func TestMagicRequestWithoutTransportIsUniformAndDisabled(t *testing.T) {
	a := newAppCfg(t, func(c *config.Config) { c.SMTP.From = "noreply@localhost.example" })
	if a.MailQueue == nil {
		t.Fatal("la file doit exister dès qu'une adresse d'expédition est configurée")
	}
	if err := a.mail.Send(context.Background(), ports.EmailMessage{To: "a@example.org", Subject: "s", Body: "b"}); err != ErrMailDisabled {
		t.Fatalf("envoi sans transport : %v, attendu ErrMailDisabled", err)
	}
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	known := b.post("/famille/connexion", "/famille/connexion/lien-magique", url.Values{"email": {"lea@example.org"}})
	unknown := newBrowser(t, a).post("/famille/connexion", "/inconnu/connexion/lien-magique", url.Values{"email": {"lea@example.org"}})
	a.Magic.Wait()
	if known.Code != http.StatusOK || unknown.Code != http.StatusOK {
		t.Errorf("statuts %d et %d, attendu 200", known.Code, unknown.Code)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM mail_queue`).Scan(&n); err != nil || n != 0 {
		t.Errorf("file non vide sans transport : %d (%v)", n, err)
	}
}

// Avec un transport, la demande de lien magique met le courriel du modèle
// MagicLink en file, dans la langue du participant, avec le lien du groupe.
func TestMagicRequestEnqueuesTemplatedMail(t *testing.T) {
	tr := &fakeTransport{}
	old := mailTransport
	mailTransport = func(*App) (queue.Transport, error) { return tr, nil }
	t.Cleanup(func() { mailTransport = old })

	a := newAppCfg(t, func(c *config.Config) { c.SMTP.From = "noreply@localhost.example" })
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")
	if _, err := a.Participants.Create(context.Background(), participants.AdminActor(g.ID), g.ID,
		participants.Input{FirstName: "Léa", Name: "Martin", Email: "Lea@Example.org", Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	rec := b.post("/famille/connexion", "/famille/connexion/lien-magique", url.Values{"email": {"lea@example.org"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("statut %d", rec.Code)
	}
	a.Magic.Wait()
	if n, err := a.MailQueue.Tick(context.Background()); err != nil || n != 1 {
		t.Fatalf("envois %d (%v), attendu 1", n, err)
	}
	if len(tr.sent) != 1 {
		t.Fatalf("enveloppes : %d", len(tr.sent))
	}
	raw := string(tr.sent[0].Raw)
	// Le corps est en quoted-printable : on cherche ce qui survit au codage.
	for _, want := range []string{"To: ", "lea@example.org", "Hello", "/famille/connexion/lien/"} {
		if !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(raw, "=\r\n", ""), "=3D", "="), want) {
			t.Errorf("%q absent du courriel :\n%s", want, raw)
		}
	}
}

// Les tâches de fond partent avec Start et s'arrêtent avec Close.
func TestOnStartRunsAndCloseWaits(t *testing.T) {
	a := newApp(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	a.OnStart(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	})
	a.Start(context.Background())
	<-started
	a.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("Close doit annuler la tâche de fond et l'attendre")
	}
}

func TestWave5ServicesBuilt(t *testing.T) {
	a := newApp(t)
	if a.GroupSettings == nil || a.DrawState == nil || a.Draw == nil || a.Enrolment == nil || a.RuleChange == nil ||
		a.Magic == nil || a.GroupPages == nil || a.AccessPages == nil {
		t.Fatalf("service manquant : %+v", a)
	}
	if a.MailQueue != nil {
		t.Error("la file d'envoi ne doit pas exister sans adresse d'expédition")
	}
	// Aucune inscription en attente tant que la fonctionnalité n'existe pas.
	n, err := noPendingRegistrations{}.PendingCount(context.Background(), 1)
	if err != nil || n != 0 {
		t.Errorf("PendingCount %d (%v)", n, err)
	}
	// Un tirage non fait pour un groupe sans tirage.
	if done, err := a.DrawState.IsDrawDone(context.Background(), 1); err != nil || done {
		t.Errorf("IsDrawDone %v (%v)", done, err)
	}
}

func TestMigrationsAllApplyOnEmptyDatabaseAndForeignKeysResolve(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for i := 0; i < 2; i++ { // une application, puis un rejeu
		if err := Migrate(d); err != nil {
			t.Fatalf("passe %d : %v", i+1, err)
		}
	}
	rows, err := d.Query(`SELECT m.name, f."table" FROM sqlite_master m, pragma_foreign_key_list(m.name) f WHERE m.type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, target string
		if err := rows.Scan(&table, &target); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?`, target).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s référence %s, qui n'existe pas", table, target)
		}
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct public", "198.51.100.7:1234", "", "198.51.100.7"},
		{"public ignore XFF", "198.51.100.7:1234", "203.0.113.9", "198.51.100.7"},
		{"bouclage lit XFF", "127.0.0.1:1234", "203.0.113.9", "203.0.113.9"},
		{"privé lit XFF", "172.18.0.2:1234", "203.0.113.9", "203.0.113.9"},
		{"privé 10/8", "10.1.2.3:1234", "203.0.113.9", "203.0.113.9"},
		{"privé 192.168", "192.168.1.1:1234", "203.0.113.9", "203.0.113.9"},
		{"bouclage v6", "[::1]:1234", "2001:db8::5", "2001:db8::5"},
		{"ULA v6", "[fd00::2]:1234", "2001:db8::5", "2001:db8::5"},
		{"bouclage sans XFF", "127.0.0.1:1234", "", "127.0.0.1"},
		{"chaîne : le plus à droite non privé", "127.0.0.1:1234", "6.6.6.6, 203.0.113.9, 10.0.0.4", "203.0.113.9"},
		{"entrée forgée à gauche ignorée", "127.0.0.1:1234", "6.6.6.6, 203.0.113.9", "203.0.113.9"},
		{"tous privés : le plus à droite", "127.0.0.1:1234", "192.168.1.50", "192.168.1.50"},
		{"jeton illisible", "127.0.0.1:1234", "pas-une-adresse", "127.0.0.1"},
		{"illisible à gauche seulement", "127.0.0.1:1234", "pas-une-adresse, 203.0.113.9", "203.0.113.9"},
		{"vide", "127.0.0.1:1234", " ", "127.0.0.1"},
		{"mappée v4", "127.0.0.1:1234", "::ffff:203.0.113.9", "203.0.113.9"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("%s : %q, attendu %q", c.name, got, c.want)
		}
	}
}
