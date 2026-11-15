package app

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"secretsanta/internal/draw"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
)

// w9 est un groupe « noel » dont l'administrateur est connecté, sur une
// application dont la file d'envoi tourne avec un faux transport.
type w9 struct {
	t      *testing.T
	a      *App
	gid    int64
	admin  *browser
	actor  participants.Actor
	secret string
}

func newW9(t *testing.T) *w9 {
	t.Helper()
	a := wave8App(t)
	secret := createGroup(t, newBrowser(t, a), "noel", "Famille Noël")
	g, err := a.Groups.BySlug(context.Background(), "noel")
	if err != nil {
		t.Fatal(err)
	}
	return &w9{t: t, a: a, gid: g.ID, admin: adminLogin(t, a, "noel", secret), actor: participants.AdminActor(g.ID), secret: secret}
}

var inviteLinkRe = regexp.MustCompile(`/noel/rejoindre/[A-Za-z0-9_-]+`)

// enableLink active l'inscription par lien depuis l'écran d'administration
// (interrupteur) et renvoie le chemin du lien affiché.
func (e *w9) enableLink() string {
	e.t.Helper()
	page := e.admin.get("/noel/admin/inscriptions")
	if page.Code != http.StatusOK {
		e.t.Fatalf("écran des inscriptions : %d\n%s", page.Code, page.Body.String())
	}
	rec := e.admin.post("/noel/admin/inscriptions", "/noel/admin/inscriptions/mode", url.Values{"enabled": {"1"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/inscriptions?ok=enabled" {
		e.t.Fatalf("activation : %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	body := e.admin.get("/noel/admin/inscriptions?ok=enabled").Body.String()
	link := inviteLinkRe.FindString(html.UnescapeString(body))
	if link == "" {
		e.t.Fatalf("lien d'invitation absent de l'écran :\n%s", body)
	}
	return link
}

var refInPageRe = regexp.MustCompile(`name="ref" value="([^"]+)"`)

// signUp soumet le formulaire d'inscription et renvoie la référence et le code
// reçu par courriel.
func (e *w9) signUp(visitor *browser, link, first, name, email string, extra url.Values) (ref, code string) {
	e.t.Helper()
	form := url.Values{"prenom": {first}, "nom": {name}, "email": {email}}
	for k, v := range extra {
		form[k] = v
	}
	rec := visitor.post(link, link, form)
	m := refInPageRe.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil {
		e.t.Fatalf("inscription de %s : %d\n%s", email, rec.Code, rec.Body.String())
	}
	return m[1], codeInMailRe.FindStringSubmatch(queuedMail(e.t, e.a, email, "code_verification"))[1]
}

func (b *browser) hasSession(kind websec.Kind) bool {
	_, ok := b.cookies[b.a.Sessions.CookieName(kind)]
	return ok
}

// --- Inscription : session ouverte par la preuve de la boîte mail ---

// Après la vérification PAR CODE ou PAR LIEN EN UN CLIC, la personne est
// connectée ; une vérification qui ne prouve rien (code rejoué après coup,
// validation de l'administrateur sans code) n'ouvre aucune session.
func TestRegistrationVerificationOpensParticipantSession(t *testing.T) {
	e := newW9(t)
	ctx := context.Background()
	link := e.enableLink()

	// Par code.
	lea := newBrowser(t, e.a)
	ref, code := e.signUp(lea, link, "Léa", "Martin", "lea@example.org", nil)
	if lea.hasSession(websec.KindParticipant) {
		t.Fatal("session ouverte avant la vérification")
	}
	if rec := lea.get("/noel/participant"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/connexion" {
		t.Fatalf("avant la vérification : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := lea.post(link, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {"ZZZZZZ"}}); rec.Code != http.StatusUnprocessableEntity || lea.hasSession(websec.KindParticipant) {
		t.Fatalf("mauvais code : %d, session %v", rec.Code, lea.hasSession(websec.KindParticipant))
	}
	rec := lea.post(link, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}})
	if rec.Code != http.StatusOK || !strings.Contains(html.UnescapeString(rec.Body.String()), a9T(e.a, "registration.public.done.title")) {
		t.Fatalf("vérification par code : %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `href="/noel/lettre?bienvenue=1"`) || strings.Contains(rec.Body.String(), "bienvenue=1?") {
		t.Errorf("le bouton principal doit mener à /noel/lettre?bienvenue=1 (une seule fois)")
	}
	if !lea.hasSession(websec.KindParticipant) {
		t.Fatal("aucune session ouverte après la vérification par code")
	}
	if rec := lea.get("/noel/participant"); rec.Code != http.StatusOK {
		t.Fatalf("espace de la personne après la vérification : %d", rec.Code)
	}
	if lea.hasSession(websec.KindAdmin) {
		t.Error("une session administrateur a été ouverte")
	}
	// La session est celle de la personne créée, dans ce groupe.
	var pid int64
	if err := e.a.DB.QueryRow(`SELECT id FROM participants WHERE group_id = ? AND email = ?`, e.gid, "lea@example.org").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM websec_sessions WHERE kind = 'participant' AND group_id = ? AND subject = ?`,
		strconv.FormatInt(e.gid, 10), strconv.FormatInt(pid, 10)); n != 1 {
		t.Errorf("sessions de la personne : %d, attendu 1", n)
	}

	// Code rejoué par un autre navigateur : rien n'a prouvé la boîte, aucune session.
	other := newBrowser(t, e.a)
	if rec := other.post(link, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}}); rec.Code >= 500 {
		t.Fatalf("code rejoué : %d", rec.Code)
	}
	if other.hasSession(websec.KindParticipant) {
		t.Error("session ouverte par un code rejoué")
	}

	// Par lien en un clic : le GET n'ouvre rien, le POST ouvre la session.
	hugo := newBrowser(t, e.a)
	e.signUp(hugo, link, "Hugo", "Bernard", "hugo@example.org", nil)
	confirm := "/noel/inscription/confirmer/" + tokenInMail.FindStringSubmatch(queuedMail(t, e.a, "hugo@example.org", "code_verification"))[1]
	if rec := hugo.get(confirm); rec.Code != http.StatusOK || hugo.hasSession(websec.KindParticipant) {
		t.Fatalf("aperçu du lien : %d, session %v", rec.Code, hugo.hasSession(websec.KindParticipant))
	}
	if rec := hugo.post(confirm, confirm, url.Values{}); rec.Code != http.StatusOK || !hugo.hasSession(websec.KindParticipant) {
		t.Fatalf("lien en un clic : %d, session %v\n%s", rec.Code, hugo.hasSession(websec.KindParticipant), rec.Body.String())
	}
	if rec := hugo.get("/noel/participant"); rec.Code != http.StatusOK {
		t.Fatalf("espace de la personne après le lien : %d", rec.Code)
	}

	// « Valider sans code » par l'administrateur : pas de session, même si la
	// personne saisit ensuite son code encore valable.
	ines := newBrowser(t, e.a)
	inesRef, inesCode := e.signUp(ines, link, "Inès", "Moreau", "ines@example.org", nil)
	var regID int64
	if err := e.a.DB.QueryRow(`SELECT id FROM registration_pending WHERE group_id = ? AND ref = ?`, e.gid, inesRef).Scan(&regID); err != nil {
		t.Fatal(err)
	}
	validate := "/noel/admin/inscriptions/" + strconv.FormatInt(regID, 10) + "/valider"
	if rec := e.admin.post("/noel/admin/inscriptions", validate, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("validation sans code : %d\n%s", rec.Code, rec.Body.String())
	}
	if ines.hasSession(websec.KindParticipant) {
		t.Fatal("session ouverte par la validation de l'administrateur")
	}
	if list, _ := e.a.Participants.List(ctx, e.actor, e.gid); len(list) != 3 {
		t.Fatalf("%d participants, attendu 3", len(list))
	}
	if rec := ines.post(link, "/noel/inscription/code", url.Values{"ref": {inesRef}, "code": {inesCode}}); rec.Code >= 500 {
		t.Fatalf("code saisi après la validation : %d", rec.Code)
	}
	if ines.hasSession(websec.KindParticipant) {
		t.Error("session ouverte pour une inscription déjà validée par l'administrateur")
	}
	if rec := ines.get("/noel/participant"); rec.Code != http.StatusSeeOther {
		t.Errorf("espace sans session : %d, attendu 303", rec.Code)
	}
}

func a9T(a *App, key string) string { return a.Cat.T("fr", key, nil) }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- Parcours complet ---

// Parcours HTTP réel : l'administrateur active le lien depuis l'écran des
// inscriptions, une personne s'inscrit en choisissant son rôle, saisit son
// code (et se retrouve connectée), écrit sa lettre, ouvre sa boîte ; l'administrateur
// la voit dans les participants, lance le tirage depuis l'onglet Tirage ; la
// personne découvre ses cibles.
func TestWave9FullJourney(t *testing.T) {
	e := newW9(t)
	a, ctx := e.a, context.Background()

	// Les trois autres participants : Paul ne fait que recevoir.
	for _, in := range []participants.Input{
		{FirstName: "Hugo", Name: "Bernard", Email: "hugo@example.org"},
		{FirstName: "Inès", Name: "Moreau", Email: "ines@example.org"},
		{FirstName: "Paul", Name: "Garnier", Email: "paul@example.org", Role: participants.RoleReceptionSeule},
	} {
		if _, err := a.Participants.Create(ctx, e.actor, e.gid, in); err != nil {
			t.Fatal(err)
		}
	}

	// 1. L'administrateur active le lien. Le choix du rôle est proposé : son
	// interrupteur arrive avec F11e, le service est donc appelé directement.
	link := e.enableLink()
	if err := a.Invite.SetRoleChoice(ctx, e.actor, e.gid, true); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM registration_invitations WHERE group_id = ? AND enabled = 1 AND role_choice = 1`, e.gid); n != 1 {
		t.Fatalf("invitation en base : %d ligne(s) avec l'option de rôle, attendu 1", n)
	}

	// 2. Inscription avec le rôle « offre plus » : persisté en SQL avant la vérification.
	lea := newBrowser(t, a)
	ref, code := e.signUp(lea, link, "Léa", "Dupuis", "lea@example.org", url.Values{"role": {"offre_plus"}})
	var pendingRole string
	if err := a.DB.QueryRow(`SELECT role FROM registration_pending WHERE group_id = ? AND ref = ?`, e.gid, ref).Scan(&pendingRole); err != nil || pendingRole != "offre_plus" {
		t.Fatalf("rôle de l'inscription en attente : %q (%v), attendu offre_plus", pendingRole, err)
	}
	// Un rôle exclusif combiné est refusé, rien n'est enregistré.
	both := newBrowser(t, a)
	rec := both.post(link, link, url.Values{"prenom": {"Zoé"}, "nom": {"Petit"}, "email": {"zoe@example.org"}, "role": {"reception_seule", "offre_plus"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rôles exclusifs : %d, attendu 422", rec.Code)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM registration_pending WHERE email = ?`, "zoe@example.org"); n != 0 {
		t.Errorf("%d inscription(s) enregistrée(s) malgré des rôles exclusifs", n)
	}

	// 3. Le code ouvre la session ; le participant porte le rôle choisi.
	if rec := lea.post(link, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}}); rec.Code != http.StatusOK {
		t.Fatalf("code : %d\n%s", rec.Code, rec.Body.String())
	}
	var leaID int64
	var role string
	if err := a.DB.QueryRow(`SELECT id, draw_role FROM participants WHERE group_id = ? AND email = ?`, e.gid, "lea@example.org").Scan(&leaID, &role); err != nil {
		t.Fatal(err)
	}
	if role != string(participants.RoleOffrePlus) {
		t.Fatalf("rôle du participant en base : %q, attendu offre_plus", role)
	}

	// 4. Sa lettre : message puis une idée ; la boîte l'affiche « envoyée ».
	write := lea.get("/noel/lettre?bienvenue=1")
	if write.Code != http.StatusOK || !strings.Contains(html.UnescapeString(write.Body.String()), a9T(a, "letter.title.write")) {
		t.Fatalf("page d'écriture : %d\n%s", write.Code, write.Body.String())
	}
	if rec := lea.post("/noel/lettre", "/noel/lettre", url.Values{"intro": {"Je rêve d'un vélo"}}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/lettre" {
		t.Fatalf("message : %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if rec := lea.post("/noel/lettre", "/noel/lettre/idees", url.Values{"title": {"Vélo rouge"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("idée : %d\n%s", rec.Code, rec.Body.String())
	}
	if body := lea.get("/noel/lettre").Body.String(); !strings.Contains(html.UnescapeString(body), "Vélo rouge") || !strings.Contains(html.UnescapeString(body), "Je rêve d'un vélo") {
		t.Errorf("la lettre relue ne contient pas ce qui a été écrit")
	}
	box := lea.get("/noel/boite")
	if box.Code != http.StatusOK || !strings.Contains(box.Body.String(), `data-state="sent"`) ||
		!strings.Contains(html.UnescapeString(box.Body.String()), a9T(a, "mailbox.own.state.sent")) {
		t.Fatalf("boîte aux lettres : %d\n%s", box.Code, box.Body.String())
	}
	for _, want := range []string{`href="/noel/lettre"`, `href="/noel/participant"`} {
		if !strings.Contains(box.Body.String(), want) {
			t.Errorf("la boîte ne porte pas %s", want)
		}
	}
	// Sans session : renvoi vers la connexion, pour la boîte et la lettre.
	for _, p := range []string{"/noel/boite", "/noel/lettre", "/noel/lettre-recue/1"} {
		if rec := newBrowser(t, a).get(p); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/connexion" {
			t.Errorf("%s sans session : %d %q", p, rec.Code, rec.Header().Get("Location"))
		}
	}

	// 5. L'administrateur voit la personne dans les participants.
	list := e.admin.get("/noel/admin/participants")
	if list.Code != http.StatusOK || !strings.Contains(html.UnescapeString(list.Body.String()), "Léa") {
		t.Fatalf("participants : %d\n%s", list.Code, list.Body.String())
	}
	sheet := e.admin.get("/noel/admin/participants/" + strconv.FormatInt(leaID, 10))
	if sheet.Code != http.StatusOK || !strings.Contains(html.UnescapeString(sheet.Body.String()), "lea@example.org") {
		t.Fatalf("fiche : %d\n%s", sheet.Code, sheet.Body.String())
	}

	// 6. Tirage depuis l'onglet : confirmation exigée, puis lancement.
	page := e.admin.get("/noel/admin/tirage")
	if page.Code != http.StatusOK {
		t.Fatalf("onglet Tirage : %d\n%s", page.Code, page.Body.String())
	}
	if rec := e.admin.post("/noel/admin/tirage", "/noel/admin/tirage/lancer", url.Values{}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("lancement sans confirmation : %d, attendu 422", rec.Code)
	}
	rec = e.admin.post("/noel/admin/tirage", "/noel/admin/tirage/lancer", url.Values{"confirm": {"1"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/tirage?ok=lance" {
		t.Fatalf("lancement : %d %q\n%s", rec.Code, rec.Header().Get("Location"), html.UnescapeString(rec.Body.String()))
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id WHERE d.status = 'actuel' AND p.giver_id = ?`, leaID); n != 2 {
		t.Errorf("paires données par la personne « offre plus » : %d, attendu 2", n)
	}
	// La révélation a suivi par Drawing : toute paire actuelle est ouverte, une seule fois.
	if n := countWhere(t, a, `SELECT COUNT(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.status = 'actuel' AND NOT EXISTS (SELECT 1 FROM reveal_targets t WHERE t.pair_id = p.id)`); n != 0 {
		t.Errorf("%d paires sans ouverture de révélation", n)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM admin_audit WHERE group_id = ? AND action = 'tirage_lance'`, e.gid); n != 1 {
		t.Errorf("tirage_lance au journal : %d, attendu 1", n)
	}
	if body := html.UnescapeString(e.admin.get("/noel/admin/journal").Body.String()); !strings.Contains(body, a9T(a, "admin.dashboard.action.tirage_lance")) {
		t.Errorf("le journal n'affiche pas le tirage lancé")
	}
	if after := e.admin.get("/noel/admin/inscriptions"); after.Code != http.StatusOK {
		t.Errorf("écran des inscriptions après le tirage : %d", after.Code)
	}

	// 7. Révélation : la personne découvre ses deux cibles ; sa boîte les montre.
	discover := lea.get("/noel/decouverte")
	if discover.Code != http.StatusOK {
		t.Fatalf("découverte : %d", discover.Code)
	}
	token := html.UnescapeString(dataCSRFRe.FindStringSubmatch(discover.Body.String())[1])
	code2, body := targetsOf2(t, lea)
	ids := targetIDRe.FindAllStringSubmatch(body, -1)
	if code2 != http.StatusOK || len(ids) != 2 {
		t.Fatalf("cibles : %d %s", code2, body)
	}
	for _, m := range ids {
		if rec := lea.postJSON("/noel/decouverte/confirmation", token, `{"target_id":`+m[1]+`}`); rec.Code != http.StatusOK {
			t.Fatalf("découverte de la cible %s : %d %s", m[1], rec.Code, rec.Body.String())
		}
	}
	box = lea.get("/noel/boite")
	if n := strings.Count(box.Body.String(), `class="mb-entry-title mb-recv"`); box.Code != http.StatusOK || n != 2 {
		t.Fatalf("boîte après la découverte : %d, %d lettre(s) reçue(s), attendu 2", box.Code, n)
	}
	if rec := lea.get("/noel/lettre-recue/" + ids[0][1]); rec.Code != http.StatusOK {
		t.Errorf("lettre reçue : %d", rec.Code)
	}
}

// targetsOf2 lit la liste JSON des cibles du groupe « noel ».
func targetsOf2(t *testing.T, b *browser) (int, string) {
	t.Helper()
	rec := b.get("/noel/decouverte/cibles")
	return rec.Code, rec.Body.String()
}

var navActiveRe = regexp.MustCompile(`<a class="admin-nav-item"[^>]*aria-current="page"`)

// --- Administration : session du bon groupe ---

// Les pages de la vague 9 exigent la session administrateur de LEUR groupe
// (jamais celle d'un autre, jamais une session participant), portent la barre
// de navigation à cinq entrées avec {slug} résolu, une CSP stricte, et leurs
// écritures sont refusées sans session.
func TestWave9AdminPagesRequireOwnGroupSession(t *testing.T) {
	e := newW9(t)
	a := e.a
	otherSecret := createGroup(t, newBrowser(t, a), "amis", "Les amis")
	other := adminLogin(t, a, "amis", otherSecret)
	p, err := a.Participants.Create(context.Background(), e.actor, e.gid, participants.Input{FirstName: "Léa", Name: "Dupuis", Email: "lea@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(p.ID, 10)

	pages := []struct {
		path, titleKey, active string
	}{
		{"/noel/admin/inscriptions", "registration.admin.title", "/noel/admin/inscriptions"},
		{"/noel/admin/participants", "admin.people.title", "/noel/admin/participants"},
		{"/noel/admin/participants/" + id, "", "/noel/admin/participants"},
		{"/noel/admin/participants/" + id + "/modifier", "", "/noel/admin/participants"},
		{"/noel/admin/tirage", "admin.draw.title", "/noel/admin/tirage"},
	}
	for _, pg := range pages {
		if rec := newBrowser(t, a).get(pg.path); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
			t.Errorf("%s sans session : %d %q", pg.path, rec.Code, rec.Header().Get("Location"))
		}
		// La session d'un autre groupe ne vaut rien ici.
		if rec := other.get(pg.path); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
			t.Errorf("%s avec la session d'un autre groupe : %d %q", pg.path, rec.Code, rec.Header().Get("Location"))
		}
		// Une session participant ne vaut rien non plus.
		tok, err := a.Magic.Issue(context.Background(), e.gid, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		part := newBrowser(t, a)
		if rec := part.post("/noel/connexion/lien/"+tok, "/noel/connexion/lien/"+tok, url.Values{}); rec.Code != http.StatusSeeOther {
			t.Fatalf("connexion participant : %d", rec.Code)
		}
		if rec := part.get(pg.path); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
			t.Errorf("%s avec une session participant : %d %q", pg.path, rec.Code, rec.Header().Get("Location"))
		}

		rec := e.admin.get(pg.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s : %d\n%s", pg.path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if pg.titleKey != "" && !strings.Contains(html.UnescapeString(body), a9T(a, pg.titleKey)) {
			t.Errorf("%s : titre %q absent (contenu d'une autre page ?)", pg.path, pg.titleKey)
		}
		if strings.Contains(body, "{slug}") || strings.Contains(strings.ToLower(body), "%7bslug%7d") {
			t.Errorf("%s : {slug} non résolu", pg.path)
		}
		if n := strings.Count(body, `<a class="admin-nav-item"`); n != 5 {
			t.Errorf("%s : %d entrées de navigation, attendu 5", pg.path, n)
		}
		for _, d := range []string{"/noel/admin", "/noel/admin/participants", "/noel/admin/tirage", "/noel/admin/inscriptions", "/noel/admin/plus"} {
			if !strings.Contains(body, `href="`+d+`"`) {
				t.Errorf("%s : entrée de navigation %s absente", pg.path, d)
			}
		}
		// Une seule entrée active dans la barre (les onglets d'une page ont leur
		// propre marque).
		if n := strings.Count(body, `<a class="admin-nav-item" href="`+pg.active+`" aria-current="page">`); n != 1 {
			t.Errorf("%s : entrée active %s absente de la barre", pg.path, pg.active)
		}
		if n := len(navActiveRe.FindAllString(body, -1)); n != 1 {
			t.Errorf("%s : %d entrée(s) active(s) dans la barre, attendu 1", pg.path, n)
		}

		if csp := rec.Header().Get("Content-Security-Policy"); csp == "" || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s : CSP %q", pg.path, csp)
		}
		if inlineRe.MatchString(body) {
			t.Errorf("%s : script ou style en ligne", pg.path)
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("%s : Cache-Control %q", pg.path, rec.Header().Get("Cache-Control"))
		}
	}

	// Écritures : sans session (ou avec celle d'un autre groupe), aucun effet.
	for _, who := range []*browser{newBrowser(t, a), other} {
		for _, w := range []struct {
			path string
			form url.Values
		}{
			{"/noel/admin/inscriptions/mode", url.Values{"enabled": {"1"}}},
			{"/noel/admin/tirage/lancer", url.Values{"confirm": {"1"}}},
			{"/noel/admin/tirage/ouvrir", url.Values{"confirm": {"1"}}},
			{"/noel/admin/participants", url.Values{"first_name": {"Zoé"}, "name": {"Petit"}}},
			{"/noel/admin/participants/" + id + "/supprimer", url.Values{"confirm": {"1"}}},
		} {
			rec := who.post("/noel/admin/connexion", w.path, w.form)
			if rec.Code == http.StatusOK || (rec.Code == http.StatusSeeOther && rec.Header().Get("Location") != "/noel/admin/connexion") {
				t.Errorf("POST %s sans la bonne session : %d %q", w.path, rec.Code, rec.Header().Get("Location"))
			}
		}
	}
	if done, _ := a.DrawState.IsDrawDone(context.Background(), e.gid); done {
		t.Error("un tirage a été lancé sans session administrateur")
	}
	if inv, _ := a.Invite.Settings(context.Background(), e.actor, e.gid); inv.Enabled {
		t.Error("l'inscription par lien a été activée sans session administrateur")
	}
	if list, _ := a.Participants.List(context.Background(), e.actor, e.gid); len(list) != 1 {
		t.Errorf("%d participants, attendu 1", len(list))
	}
}

// --- Cohérence des liens ---

var (
	internalLinkRe = regexp.MustCompile(`(?:href|action|src|data-[a-z-]+)="(/[^"]*)"`)
	numericSegRe   = regexp.MustCompile(`/\d+(/|$)`)
)

// futureRoutes sont les pages que des liens par défaut désignent déjà mais qui
// viennent dans une vague suivante (motifs : le groupe est écrit {slug}, un
// identifiant {id}). Toute autre adresse émise par une page doit monter sur
// une route existante. wildcard est vrai quand un motif existant (par exemple
// /{slug}/admin/participants/{id}) retient déjà l'adresse sans la servir.
var futureRoutes = map[string]struct{ wildcard bool }{
	"/{slug}/admin/ajuster":  {}, // ajustement du tirage (F10c)
	"/{slug}/admin/reglages": {}, // réglages du groupe
	"/{slug}/admin/aide":     {}, // aide de l'administration
}

func normalizeLink(p string) string {
	p = strings.Replace(p, "/noel/", "/{slug}/", 1)
	p = strings.Replace(p, "/amis/", "/{slug}/", 1)
	for numericSegRe.MatchString(p) {
		p = numericSegRe.ReplaceAllString(p, "/{id}$1")
	}
	return p
}

// routed dit si une route (méthode GET ou POST) retient l'adresse, comme le
// fait App.routes : routes fixes d'abord, routes par groupe ensuite.
func (a *App) routed(p string) bool {
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(m, p, nil)
		if _, pat := a.Mux.Handler(req); pat != "" {
			return true
		}
		if _, pat := a.Slugs.Handler(req); pat != "" {
			return true
		}
	}
	return false
}

// Chaque lien interne émis par les pages (liens, actions de formulaire, adresses
// données aux scripts) monte sur une route existante ou sur une page connue
// « à venir » (futureRoutes) : aucune faute de chemin (boite pour
// boite-aux-lettres, etc.). Les pages sont lues avant et après le tirage, en
// administrateur, en participant et en visiteur.
func TestDefaultInternalLinksReachARoute(t *testing.T) {
	e := newW9(t)
	a, ctx := e.a, context.Background()
	seen := map[string]map[string]bool{} // adresse normalisée -> pages qui l'émettent
	pages := 0
	crawl := func(who *browser, path string) {
		t.Helper()
		rec := who.get(path)
		if rec.Code != http.StatusOK && rec.Code != http.StatusConflict {
			t.Fatalf("%s : statut %d\n%s", path, rec.Code, rec.Body.String())
		}
		pages++
		scan(seen, path, rec.Body.String())
	}

	var ids []int64
	for _, in := range []participants.Input{
		{FirstName: "Hugo", Name: "Bernard", Email: "hugo@example.org"},
		{FirstName: "Inès", Name: "Moreau", Email: "ines@example.org"},
		{FirstName: "Paul", Name: "Garnier", Email: "paul@example.org", Role: participants.RoleReceptionSeule},
	} {
		p, err := a.Participants.Create(ctx, e.actor, e.gid, in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	link := e.enableLink()

	// Visiteur : formulaire, saisie du code, aperçu du lien, réussite.
	anon := newBrowser(t, a)
	for _, p := range []string{"/", "/creer", "/noel", "/noel/connexion", "/noel/admin/connexion", link} {
		crawl(anon, p)
	}
	rec := anon.post(link, link, url.Values{"prenom": {"Zoé"}, "nom": {"Petit"}, "email": {"zoe@example.org"}})
	scan(seen, "POST "+link, rec.Body.String())
	rec = anon.post(link, link, url.Values{"prenom": {""}, "nom": {"Petit"}, "email": {"x"}})
	scan(seen, "POST invalide "+link, rec.Body.String())
	confirm := "/noel/inscription/confirmer/" + tokenInMail.FindStringSubmatch(queuedMail(t, a, "zoe@example.org", "code_verification"))[1]
	crawl(anon, confirm)
	pages += 2
	lea := newBrowser(t, a)
	ref, code := e.signUp(lea, link, "Léa", "Dupuis", "lea@example.org", nil)
	rec = lea.post(link, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}})
	scan(seen, "success", rec.Body.String())
	pages++
	rec = lea.post(link, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {"ZZZZZZ"}})
	scan(seen, "code refusé", rec.Body.String())
	pages++
	var leaID int64
	if err := a.DB.QueryRow(`SELECT id FROM participants WHERE group_id = ? AND email = 'lea@example.org'`, e.gid).Scan(&leaID); err != nil {
		t.Fatal(err)
	}
	leaS := strconv.FormatInt(leaID, 10)

	adminPages := []string{
		"/noel/admin", "/noel/admin/journal", "/noel/admin/plus", "/noel/admin/roles",
		"/noel/admin/inscriptions", "/noel/admin/participants", "/noel/admin/participants/" + leaS,
		"/noel/admin/participants/" + leaS + "/modifier", "/noel/admin/tirage",
	}
	memberPages := []string{"/noel/participant", "/noel/lettre", "/noel/lettre?bienvenue=1", "/noel/boite", "/noel/decouverte"}
	for _, p := range append(adminPages, "/noel/admin/controle") {
		crawl(e.admin, p)
	}
	for _, p := range memberPages {
		crawl(lea, p)
	}
	if _, err := a.Letter.SetIntro(ctx, e.gid, leaID, "Un vélo"); err != nil {
		t.Fatal(err)
	}

	// Après le tirage (la personne « ignorée » ne bloque plus), révélation ouverte.
	if err := a.Draw.ResolvePending(ctx, e.actor, e.gid, ports.ResolveIgnore); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Drawing.Draw(ctx, e.actor, e.gid, draw.Request{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range append(adminPages, "/noel/admin/controle") {
		crawl(e.admin, p)
	}
	for _, id := range ids {
		crawl(e.admin, "/noel/admin/participants/"+strconv.FormatInt(id, 10))
	}
	token := html.UnescapeString(dataCSRFRe.FindStringSubmatch(lea.get("/noel/decouverte").Body.String())[1])
	_, body := targetsOf2(t, lea)
	for _, m := range targetIDRe.FindAllStringSubmatch(body, -1) {
		if rec := lea.postJSON("/noel/decouverte/confirmation", token, `{"target_id":`+m[1]+`}`); rec.Code != http.StatusOK {
			t.Fatalf("découverte : %d", rec.Code)
		}
		crawl(lea, "/noel/lettre-recue/"+m[1])
	}
	for _, p := range memberPages {
		crawl(lea, p)
	}
	crawl(lea, "/noel/ma-cible")

	if pages < 40 {
		t.Fatalf("seulement %d pages lues : le test ne vérifierait presque rien", pages)
	}
	var bad []string
	emitted := 0
	for _, link := range sortedKeys2(seen) {
		pat := normalizeLink(link)
		if strings.Contains(link, "{") || strings.Contains(strings.ToLower(link), "%7b") {
			bad = append(bad, link+" : joker non résolu, émis par "+strings.Join(sortedKeys(seen[link]), ", "))
			continue
		}
		emitted++
		fut, isFuture := futureRoutes[pat]
		switch routed := a.routed(link); {
		case isFuture && routed && !fut.wildcard:
			bad = append(bad, link+" : la page « à venir » existe maintenant, la retirer de futureRoutes")
		case !isFuture && !routed:
			bad = append(bad, link+" : aucune route, émis par "+strings.Join(sortedKeys(seen[link]), ", "))
		}
	}
	if emitted < 25 {
		t.Fatalf("%d liens seulement : l'extraction ne vérifierait presque rien", emitted)
	}
	if len(bad) > 0 {
		t.Errorf("liens internes sans route :\n%s", strings.Join(bad, "\n"))
	}
}

func scan(seen map[string]map[string]bool, page, body string) {
	for _, m := range internalLinkRe.FindAllStringSubmatch(html.UnescapeString(body), -1) {
		l := m[1]
		if i := strings.IndexAny(l, "?#"); i >= 0 {
			l = l[:i]
		}
		if strings.HasPrefix(l, "/static/") || strings.HasPrefix(l, "/images/") || l == "/" || strings.HasPrefix(l, "//") {
			continue
		}
		if seen[l] == nil {
			seen[l] = map[string]bool{}
		}
		seen[l][page] = true
	}
}

func sortedKeys2[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
