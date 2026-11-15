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
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/draw"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/transport"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
	"secretsanta/internal/reveal"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/scheduler"
)

// withSMTP active l'adresse d'expédition et un serveur SMTP (jamais contacté).
func withSMTP(c *config.Config) {
	c.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, From: "noreply@localhost.example"}
	c.Mail = config.Mail{TLSMode: config.TLSNone, AllowInsecure: true}
}

// useFakeTransport remplace le transport d'envoi le temps du test.
func useFakeTransport(t *testing.T) *fakeTransport {
	t.Helper()
	tr := &fakeTransport{}
	old := mailTransport
	mailTransport = func(*App) (queue.Transport, error) { return tr, nil }
	t.Cleanup(func() { mailTransport = old })
	return tr
}

// --- Transport SMTP ---

func TestSMTPTransportFromConfig(t *testing.T) {
	a := newApp(t)
	if tr, err := mailTransport(a); err != nil || tr != nil {
		t.Fatalf("sans SMTP_HOST le transport doit être nil : %v (%v)", tr, err)
	}
	a = newAppCfg(t, withSMTP)
	tr, err := mailTransport(a)
	if err != nil || tr == nil {
		t.Fatalf("avec SMTP_HOST le transport doit exister : %v (%v)", tr, err)
	}
	if _, ok := tr.(*transport.Transport); !ok {
		t.Fatalf("transport %T, attendu *transport.Transport", tr)
	}
	if a.MailQueue == nil || a.mail.q == nil {
		t.Fatal("la file doit tourner dès qu'un transport existe")
	}
}

func TestMailQueueRunsWithStartAndStopsWithClose(t *testing.T) {
	tr := useFakeTransport(t)
	a := newAppCfg(t, withSMTP)
	if _, err := a.mail.q.EnqueueMail(context.Background(), queue.Item{
		To: "lea@example.org", ToName: "Léa", Type: "test", Essential: true, Subject: "Essai", Text: "Bonjour\n", HTML: "<p>Bonjour</p>",
	}); err != nil {
		t.Fatal(err)
	}
	a.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tr.mu.Lock()
		n := len(tr.sent)
		tr.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.Close() // attend la file : rien ne tourne après
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.sent) != 1 {
		t.Fatalf("envois %d, attendu 1 (la file doit démarrer avec Start)", len(tr.sent))
	}
}

// --- Désinscription ---

func TestUnsubMigrationAndRoutes(t *testing.T) {
	a := newApp(t)
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = 'mail_unsub'`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("module mail_unsub non appliqué (%d, %v)", n, err)
	}
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM mail_unsubscribe`).Scan(&n); err != nil {
		t.Fatalf("table mail_unsubscribe absente : %v", err)
	}
	b := newBrowser(t, a)
	createGroup(t, b, "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")
	p, err := a.Participants.Create(context.Background(), participants.AdminActor(g.ID), g.ID,
		participants.Input{FirstName: "Léa", Name: "Martin", Email: "lea@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := a.Unsub.URL(g.ID, p.ID, "relances")
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(link, "http://localhost:8080")
	if !strings.HasPrefix(path, "/desinscription/") {
		t.Fatalf("lien inattendu : %s", link)
	}

	// GET : page de confirmation, rien n'est changé.
	rec := b.get(path)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Famille Martin") {
		t.Fatalf("GET : statut %d\n%s", rec.Code, rec.Body.String())
	}
	if off, _ := a.Unsub.IsUnsubscribed(context.Background(), p.ID, "relances"); off {
		t.Fatal("le GET a désinscrit")
	}
	// Signature fausse : 404 neutre.
	if rec := b.get(path + "x"); rec.Code != http.StatusNotFound {
		t.Errorf("signature fausse : statut %d, attendu 404", rec.Code)
	}
	// POST « un clic » d'un client de messagerie : ni cookie, ni origine, ni
	// jeton CSRF. Il est exempté de la chaîne CSRF, et seulement lui.
	oneClick := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("List-Unsubscribe=One-Click"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = testClientAddr
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := oneClick(path); rec.Code != http.StatusOK {
		t.Fatalf("POST un clic : statut %d\n%s", rec.Code, rec.Body.String())
	}
	if off, err := a.Unsub.IsUnsubscribed(context.Background(), p.ID, "relances"); err != nil || !off {
		t.Fatalf("désinscription non enregistrée (%v, %v)", off, err)
	}
	if rec := oneClick("/health"); rec.Code != http.StatusForbidden {
		t.Errorf("POST hors désinscription sans CSRF : statut %d, attendu 403", rec.Code)
	}
	if rec := oneClick(mailunsub.RoutePrefix + "jeton-faux"); rec.Code != http.StatusNotFound {
		t.Errorf("POST signature fausse : statut %d, attendu 404", rec.Code)
	}
}

// --- Suggestions ---

func TestSuggestionServicesGoThroughSSRFGuard(t *testing.T) {
	a := newAppCfg(t, func(c *config.Config) { c.Sources.SearxngURL = "http://127.0.0.1:8888" })
	if a.Sources == nil || a.Enrich == nil {
		t.Fatalf("services de suggestions manquants : %v %v", a.Sources, a.Enrich)
	}
	names := strings.Join(a.Sources.Names(), ",")
	if !strings.Contains(names, "searxng") {
		t.Errorf("la source SearXNG configurée est absente : %s", names)
	}
	if a.Enrich.Fetcher != any(a.SSRF) {
		t.Error("l'enrichissement doit passer par la garde SSRF")
	}
	if a.Enrich.Images == nil {
		t.Error("l'enrichissement doit enregistrer ses images dans le magasin local")
	}
}

// --- Révélation ---

const revealSlug = "famille"

// revealEnv est un groupe de quatre personnes prêt à être tiré.
type revealEnv struct {
	t     *testing.T
	a     *App
	gid   int64
	ids   []int64
	names []string
	admin participants.Actor
}

func newRevealEnv(t *testing.T, a *App) *revealEnv {
	t.Helper()
	b := newBrowser(t, a)
	createGroup(t, b, revealSlug, "Famille Martin")
	g, err := a.Groups.BySlug(context.Background(), revealSlug)
	if err != nil {
		t.Fatal(err)
	}
	e := &revealEnv{t: t, a: a, gid: g.ID, admin: participants.AdminActor(g.ID)}
	for _, in := range []participants.Input{
		{FirstName: "Léa", Name: "Dupuis", Email: "lea@example.org", GiftHint: "Aime le thé"},
		{FirstName: "Hugo", Name: "Bernard", Email: "hugo@example.org"},
		{FirstName: "Inès", Name: "Moreau", Email: "ines@example.org"},
		{FirstName: "Paul", Name: "Garnier", Email: "paul@example.org"},
	} {
		p, err := a.Participants.Create(context.Background(), e.admin, g.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		e.ids = append(e.ids, p.ID)
		e.names = append(e.names, strings.TrimSpace(p.FirstName+" "+p.Name))
	}
	return e
}

func (e *revealEnv) setRevealDate(at time.Time) {
	e.t.Helper()
	if err := e.a.Reveal.SetRevealDate(context.Background(), e.admin, e.gid, &at); err != nil {
		e.t.Fatal(err)
	}
}

func (e *revealEnv) drawNow() {
	e.t.Helper()
	if _, err := e.a.Drawing.Draw(context.Background(), e.admin, e.gid, draw.Request{Confirmed: true}); err != nil {
		e.t.Fatal(err)
	}
}

// login ouvre la session participant de la personne i dans un navigateur neuf.
func (e *revealEnv) login(i int) *browser {
	e.t.Helper()
	tok, err := e.a.Magic.Issue(context.Background(), e.gid, e.ids[i])
	if err != nil {
		e.t.Fatal(err)
	}
	link := "/" + revealSlug + "/connexion/lien/" + tok
	b := newBrowser(e.t, e.a)
	if rec := b.post(link, link, url.Values{}); rec.Code != http.StatusSeeOther {
		e.t.Fatalf("connexion : statut %d", rec.Code)
	}
	return b
}

var dataCSRFRe = regexp.MustCompile(`data-csrf="([^"]+)"`)

// postJSON envoie un corps JSON avec le jeton CSRF de la page, comme le script.
func (b *browser) postJSON(target, token, body string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.RemoteAddr = b.addr
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-CSRF-Token", token)
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.a.Handler().ServeHTTP(rec, req)
	return rec
}

// targetsOf lit la liste JSON des cibles de la personne connectée.
func targetsOf(t *testing.T, b *browser) (int, string) {
	t.Helper()
	rec := b.get("/" + revealSlug + "/decouverte/cibles")
	return rec.Code, rec.Body.String()
}

func TestRevealMigrationAndGateWired(t *testing.T) {
	a := newApp(t)
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = ?`, reveal.Module).Scan(&n); err != nil || n == 0 {
		t.Fatalf("module reveal non appliqué (%d, %v)", n, err)
	}
	for _, table := range []string{"reveal_groups", "reveal_openings", "reveal_targets", "reveal_notices"} {
		if err := a.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Errorf("table %s absente : %v", table, err)
		}
	}
	if a.Reveal == nil || a.Drawing == nil {
		t.Fatal("service de révélation manquant")
	}
}

// Avec une date de révélation, la cible n'est jamais renvoyée avant ; elle
// l'est à la date, par la tâche planifiée, et un tirage refait repart fermé.
func TestRevealOpensAtDateAndTargetNeverRenderedBefore(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	fake := a.Clock.(*clock.Fake)
	e.setRevealDate(fake.Now().Add(48 * time.Hour))
	e.drawNow()

	b := e.login(0)
	// Page : sert la coque, sans aucune cible.
	page := b.get("/" + revealSlug + "/decouverte")
	if page.Code != http.StatusOK {
		t.Fatalf("page : statut %d", page.Code)
	}
	for _, n := range e.names[1:] {
		if strings.Contains(page.Body.String(), n) {
			t.Errorf("la page rend le nom %q", n)
		}
	}
	// Avant la date : liste vide, aucun nom nulle part.
	code, body := targetsOf(t, b)
	if code != http.StatusOK || strings.TrimSpace(body) != `{"targets":[]}` {
		t.Fatalf("avant la date : statut %d, corps %s", code, body)
	}
	for _, n := range e.names {
		if strings.Contains(body, n) {
			t.Errorf("le nom %q est rendu avant la date", n)
		}
	}
	// La confirmation est refusée tant que la porte est fermée.
	token := dataCSRFRe.FindStringSubmatch(page.Body.String())[1]
	if rec := b.postJSON("/"+revealSlug+"/decouverte/confirmation", html.UnescapeString(token), `{"target_id":1}`); rec.Code != http.StatusConflict {
		t.Errorf("confirmation avant la date : statut %d, attendu 409", rec.Code)
	}
	var openings int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_openings`).Scan(&openings)
	if openings != 0 {
		t.Fatalf("ouverture enregistrée avant la date : %d", openings)
	}

	// La date arrive : la tâche planifiée ouvre la révélation.
	fake.Advance(49 * time.Hour)
	if _, err := a.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_openings WHERE how = ?`, reveal.HowDate).Scan(&openings); err != nil || openings != 1 {
		t.Fatalf("ouvertures à la date : %d (%v), attendu 1", openings, err)
	}
	code, body = targetsOf(t, b)
	if code != http.StatusOK || strings.Contains(body, `"targets":[]`) {
		t.Fatalf("après la date : statut %d, corps %s", code, body)
	}
	if strings.Contains(body, e.names[0]) {
		t.Errorf("la personne est sa propre cible : %s", body)
	}
	named := 0
	for _, n := range e.names[1:] {
		if strings.Contains(body, `"name":"`+n+`"`) {
			named++
		}
	}
	if named != 1 {
		t.Errorf("cibles nommées : %d, attendu 1 (%s)", named, body)
	}

	// La date est verrouillée une fois la révélation ouverte, avec son code.
	err := a.Reveal.SetRevealDate(context.Background(), e.admin, e.gid, ptrTime(fake.Now().Add(time.Hour)))
	code2, _ := errs.CodeOf(err)
	if code2 != reveal.CodeDateLocked {
		t.Fatalf("date après ouverture : %v (code %q)", err, code2)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// Le code d'erreur reveal.date_locked existe en français et en anglais.
func TestRevealDateLockedCodeInCatalog(t *testing.T) {
	a := newApp(t)
	for _, lang := range []string{"fr", "en"} {
		m := a.Cat.Messages(lang)[reveal.CodeDateLocked]
		if strings.TrimSpace(m) == "" || m == reveal.CodeDateLocked {
			t.Errorf("texte %s manquant pour %s", lang, reveal.CodeDateLocked)
		}
	}
	if a.Cat.T("fr", reveal.CodeDateLocked, nil) == a.Cat.T("en", reveal.CodeDateLocked, nil) {
		t.Error("les textes fr et en sont identiques")
	}
}

// Sans date de révélation, un tirage ouvre aussitôt la révélation : la
// confirmation pose le drapeau une seule fois, et seulement pour sa cible.
func TestRevealImmediateAndConfirm(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()

	b := e.login(0)
	page := b.get("/" + revealSlug + "/decouverte")
	token := html.UnescapeString(dataCSRFRe.FindStringSubmatch(page.Body.String())[1])
	code, body := targetsOf(t, b)
	if code != http.StatusOK || strings.Contains(body, `"targets":[]`) {
		t.Fatalf("révélation immédiate : statut %d, corps %s", code, body)
	}
	var pairID int64
	if err := a.DB.QueryRow(`SELECT id FROM draw_pairs WHERE giver_id = ?`, e.ids[0]).Scan(&pairID); err != nil {
		t.Fatal(err)
	}
	var otherPair int64
	if err := a.DB.QueryRow(`SELECT id FROM draw_pairs WHERE giver_id = ?`, e.ids[1]).Scan(&otherPair); err != nil {
		t.Fatal(err)
	}
	confirm := "/" + revealSlug + "/decouverte/confirmation"
	if rec := b.postJSON(confirm, token, `{"target_id":`+itoa(otherPair)+`}`); rec.Code != http.StatusNotFound {
		t.Errorf("cible d'un autre : statut %d, attendu 404", rec.Code)
	}
	if rec := b.postJSON(confirm, token, `pas du json`); rec.Code != http.StatusBadRequest {
		t.Errorf("corps illisible : statut %d, attendu 400", rec.Code)
	}
	if rec := b.postJSON(confirm, "", `{"target_id":`+itoa(pairID)+`}`); rec.Code != http.StatusForbidden {
		t.Errorf("sans jeton CSRF : statut %d, attendu 403", rec.Code)
	}
	rec := b.postJSON(confirm, token, `{"target_id":`+itoa(pairID)+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"first":true`) {
		t.Fatalf("confirmation : statut %d, %s", rec.Code, rec.Body.String())
	}
	rec = b.postJSON(confirm, token, `{"target_id":`+itoa(pairID)+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"first":false`) {
		t.Errorf("seconde confirmation : statut %d, %s", rec.Code, rec.Body.String())
	}
	var discovered int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM draw_pairs WHERE discovered_at IS NOT NULL`).Scan(&discovered)
	if discovered != 1 {
		t.Errorf("drapeaux posés : %d, attendu 1", discovered)
	}
	// Une cible découverte verrouille la date de révélation.
	f, err := a.Reveal.Facts(context.Background(), e.admin, e.gid)
	if err != nil || f.Discovered != 1 || !f.DateLocked || !f.Open {
		t.Errorf("faits : %+v (%v)", f, err)
	}
	// GET sur la confirmation : aucune modification possible.
	if rec := b.get(confirm); rec.Code == http.StatusOK {
		t.Errorf("GET sur la confirmation accepté : %d", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Sans session, la page renvoie à la connexion et les appels JSON refusent ;
// un groupe inconnu reçoit le 404 neutre.
func TestRevealRoutesRequireParticipantSession(t *testing.T) {
	a := newApp(t)
	newRevealEnv(t, a)
	b := newBrowser(t, a)
	if rec := b.get("/" + revealSlug + "/decouverte"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/"+revealSlug+"/connexion" {
		t.Errorf("page sans session : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec := b.get("/" + revealSlug + "/decouverte/cibles")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "core.errors.unauthorized") {
		t.Errorf("cibles sans session : %d %s", rec.Code, rec.Body.String())
	}
	if rec := b.get("/groupe-inconnu/decouverte"); rec.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", rec.Code)
	}
	if rec := b.get("/groupe-inconnu/decouverte/cibles"); rec.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu (JSON) : %d", rec.Code)
	}
	// La session d'un administrateur n'est pas une session participant.
	if rec := b.post("/"+revealSlug+"/connexion", "/"+revealSlug+"/decouverte/confirmation", url.Values{}); rec.Code == http.StatusOK {
		t.Errorf("confirmation par formulaire acceptée : %d", rec.Code)
	}
}

// Le vrai gabarit html/template rend : coque complète, adresses du groupe,
// textes du catalogue dans la langue de la requête, aucune clé brute ni
// valeur rejetée par html/template.
func TestRevealPageRendersRealTemplate(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	b := e.login(0)
	page := b.get("/" + revealSlug + "/decouverte")
	body := page.Body.String()
	if page.Code != http.StatusOK {
		t.Fatalf("statut %d\n%s", page.Code, body)
	}
	for _, want := range []string{
		`<html lang="fr">`, `data-reveal`,
		`data-targets-url="/famille/decouverte/cibles"`, `data-confirm-url="/famille/decouverte/confirmation"`,
		`/static/js/reveal/core/main.js`, `/static/css/reveal/core/reveal.css`,
		a.Cat.T("fr", "reveal.core.title.page", nil), a.Cat.T("fr", "reveal.core.title.ready", nil),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent de la page", want)
		}
	}
	if m := dataCSRFRe.FindStringSubmatch(body); m == nil || strings.TrimSpace(m[1]) == "" {
		t.Error("jeton CSRF absent de data-csrf")
	}
	for _, bad := range []string{"ZgotmplZ", "reveal.core.", "&lt;no value&gt;", "<no value>", `data-letter`} {
		if strings.Contains(body, bad) {
			t.Errorf("%q dans la page : %s", bad, body)
		}
	}
	// Anglais : même page, textes anglais.
	b.headers["Accept-Language"] = "en"
	en := b.get("/" + revealSlug + "/decouverte")
	if !strings.Contains(en.Body.String(), a.Cat.T("en", "reveal.core.title.page", nil)) || strings.Contains(en.Body.String(), "reveal.core.") {
		t.Errorf("rendu anglais inattendu : %s", en.Body.String())
	}
	// CSP stricte : ni style ni script en ligne.
	if regexp.MustCompile(`(?i)<script(?: type="module")?>|\sstyle=`).MatchString(body) {
		t.Error("script ou style en ligne dans la page")
	}
}

// Après un nouveau tirage derrière une date à venir, la révélation reste
// fermée (aucune cible rendue) puis s'ouvre à la date pour le nouveau tirage.
func TestRevealRedrawStaysClosedBehindDate(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	fake := a.Clock.(*clock.Fake)
	e.setRevealDate(fake.Now().Add(24 * time.Hour))
	e.drawNow()
	if _, err := a.Drawing.Redraw(context.Background(), e.admin, e.gid, draw.Request{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	b := e.login(0)
	if code, body := targetsOf(t, b); code != http.StatusOK || strings.TrimSpace(body) != `{"targets":[]}` {
		t.Fatalf("après le nouveau tirage : %d %s", code, body)
	}
	fake.Advance(25 * time.Hour)
	if _, err := a.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var openings int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_openings o JOIN draws d ON d.id = o.draw_id AND d.status = 'actuel'`).Scan(&openings)
	if openings != 1 {
		t.Errorf("ouverture du nouveau tirage : %d, attendu 1", openings)
	}
	if code, body := targetsOf(t, b); code != http.StatusOK || strings.Contains(body, `"targets":[]`) {
		t.Errorf("à la date : %d %s", code, body)
	}
}

// Restore rattrape une ouverture due pendant un arrêt.
func TestRevealRestoreCatchesUpOverdueOpening(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	fake := a.Clock.(*clock.Fake)
	e.setRevealDate(fake.Now().Add(24 * time.Hour))
	e.drawNow()
	fake.Advance(25 * time.Hour) // la tâche n'a pas tourné (application arrêtée)
	if err := a.Reveal.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	var openings int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_openings`).Scan(&openings)
	if openings != 1 {
		t.Errorf("ouvertures après Restore : %d, attendu 1", openings)
	}
	// Au démarrage de l'application, Restore est lancé.
	a2 := newApp(t)
	called := make(chan struct{}, 1)
	a2.OnStart(func(context.Context) { called <- struct{}{} })
	a2.Start(context.Background())
	<-called
	a2.Close()
}

// Les avis « tirage prêt » : sans transport ils restent en attente ; avec un
// transport ils partent, un par personne, sans nom de cible, avec la variante
// de gabarit et le lien de la page de découverte.
func TestReadyNoticesPendingWithoutTransportThenSent(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	var pending, sent int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_notices WHERE sent_at IS NULL`).Scan(&pending)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_notices WHERE sent_at IS NOT NULL`).Scan(&sent)
	if pending != 4 || sent != 0 {
		t.Fatalf("sans transport : %d en attente, %d envoyés, attendu 4 et 0", pending, sent)
	}
}

func TestReadyNoticesEnqueuedWithTransport(t *testing.T) {
	tr := useFakeTransport(t)
	a := newAppCfg(t, withSMTP)
	e := newRevealEnv(t, a)
	e.drawNow()
	var queued, sent int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mail_queue WHERE mail_type = 'ready_one'`).Scan(&queued)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_notices WHERE sent_at IS NOT NULL`).Scan(&sent)
	if queued != 4 || sent != 4 {
		t.Fatalf("avis en file %d, marqués envoyés %d, attendu 4 et 4", queued, sent)
	}
	if n, err := a.MailQueue.Tick(context.Background()); err != nil || n != 4 {
		t.Fatalf("envois %d (%v), attendu 4", n, err)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	flat := func(raw []byte) string {
		return strings.ReplaceAll(strings.ReplaceAll(string(raw), "=\r\n", ""), "=3D", "=")
	}
	for _, env := range tr.sent {
		raw := flat(env.Raw)
		if !strings.Contains(raw, "/famille/decouverte") {
			t.Errorf("lien de découverte absent :\n%s", raw)
		}
		for _, n := range e.names {
			parts := strings.Fields(n)
			if strings.Contains(raw, parts[1]) { // nom de famille d'une personne
				t.Errorf("le courriel cite le nom %q", parts[1])
			}
		}
	}
}

// Un participant supprimé après l'ouverture n'empêche pas l'envoi des autres
// avis : EnqueueReady renvoie nil pour lui.
func TestReadyMailerSkipsDeletedParticipant(t *testing.T) {
	useFakeTransport(t)
	a := newAppCfg(t, withSMTP)
	e := newRevealEnv(t, a)
	m := readyMailer{q: a.mail.q, cat: a.Cat, groups: a.Groups, settings: a.GroupSettings, parts: a.Participants, base: a.publicBase()}
	if err := m.EnqueueReady(context.Background(), reveal.ReadyNotice{GroupID: e.gid, DrawID: 1, ParticipantID: 99999, Targets: 1}); err != nil {
		t.Fatalf("participant inconnu : %v, attendu nil", err)
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mail_queue`).Scan(&n)
	if n != 0 {
		t.Errorf("courriel mis en file pour un participant inconnu : %d", n)
	}
	// Variantes : réception seule, une et deux cibles.
	for _, nt := range []reveal.ReadyNotice{
		{GroupID: e.gid, ParticipantID: e.ids[0], Targets: 0, ReceptionOnly: true},
		{GroupID: e.gid, ParticipantID: e.ids[1], Targets: 1},
		{GroupID: e.gid, ParticipantID: e.ids[2], Targets: 2},
	} {
		if err := m.EnqueueReady(context.Background(), nt); err != nil {
			t.Fatalf("%+v : %v", nt, err)
		}
	}
	for _, kind := range []string{"ready_reception", "ready_one", "ready_two"} {
		_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mail_queue WHERE mail_type = ?`, kind).Scan(&n)
		if n != 1 {
			t.Errorf("variante %s : %d courriel(s), attendu 1", kind, n)
		}
	}
}

// Ajustement : la révélation suit (une cible ajoutée à un tirage déjà ouvert
// est ouverte aussi).
func TestDrawingAdjustOpensNewTargets(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	rows, err := a.DB.Query(`SELECT giver_id, receiver_id FROM draw_pairs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var pairs []draw.Pair
	for rows.Next() {
		var p draw.Pair
		if err := rows.Scan(&p.Giver, &p.Receiver); err != nil {
			t.Fatal(err)
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := a.Drawing.Adjust(context.Background(), participants.ParticipantActor(e.gid, e.ids[0]), e.gid, pairs); !errs.Is(err, errs.KindForbidden) {
		t.Fatalf("ajustement par un participant : %v, attendu un refus", err)
	}
	// Deux receveurs échangés entre deux tireurs (sans cadeau à soi-même).
	swapped := false
	for i := 0; i < len(pairs) && !swapped; i++ {
		for j := i + 1; j < len(pairs) && !swapped; j++ {
			if pairs[i].Giver != pairs[j].Receiver && pairs[j].Giver != pairs[i].Receiver {
				pairs[i].Receiver, pairs[j].Receiver = pairs[j].Receiver, pairs[i].Receiver
				swapped = true
			}
		}
	}
	if !swapped {
		t.Fatal("aucun échange possible")
	}
	if err := a.Drawing.Adjust(context.Background(), e.admin, e.gid, pairs); err != nil {
		t.Fatal(err)
	}
	var pairsN, targetsN int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id AND d.status = 'actuel'`).Scan(&pairsN)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM reveal_targets`).Scan(&targetsN)
	if pairsN != targetsN {
		t.Errorf("paires %d, cibles ouvertes %d : la révélation n'a pas suivi l'ajustement", pairsN, targetsN)
	}
}

// Les ports restent ceux attendus.
var (
	_ ports.EmailSender = mailOutbox{}
	_ scheduler.Task    = func(context.Context) error { return nil }
)
