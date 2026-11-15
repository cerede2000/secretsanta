package app

import (
	"bytes"
	"context"
	"html"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/adjust"
	"secretsanta/internal/admin/audit"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/letter"
	"secretsanta/internal/letterread"
	"secretsanta/internal/participants"
	regmig "secretsanta/internal/registration/migrations"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/targetpage"
)

var inlineRe = regexp.MustCompile(`(?i)<script(?: type="module")?>|\sstyle=`)

// wave8App construit une application dont la file d'envoi tourne avec un faux
// transport : les courriels se lisent dans mail_queue.
func wave8App(t *testing.T) *App {
	t.Helper()
	useFakeTransport(t)
	return newAppCfg(t, withSMTP)
}

// queuedMail renvoie le texte du dernier courriel du type donné adressé à to.
func queuedMail(t *testing.T, a *App, to, mailType string) string {
	t.Helper()
	var body string
	err := a.DB.QueryRow(`SELECT body_text FROM mail_queue WHERE recipient = ? AND mail_type = ? ORDER BY id DESC LIMIT 1`,
		to, mailType).Scan(&body)
	if err != nil {
		t.Fatalf("courriel %s à %s introuvable : %v", mailType, to, err)
	}
	return body
}

func countWhere(t *testing.T, a *App, query string, args ...any) int {
	t.Helper()
	var n int
	if err := a.DB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s : %v", query, err)
	}
	return n
}

func adminLogin(t *testing.T, a *App, slug, secret string) *browser {
	t.Helper()
	b := newBrowser(t, a)
	rec := b.post("/"+slug+"/admin/connexion", "/"+slug+"/admin/connexion", url.Values{"secret": {secret}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/"+slug+"/admin" {
		t.Fatalf("connexion administrateur de %s : %d %q", slug, rec.Code, rec.Header().Get("Location"))
	}
	return b
}

// --- Migrations ---

// Les quatre modules de la vague 8 (et celui de l'intégration) sont appliqués
// après leurs dépendances.
func TestWave8MigrationsAfterTheirDependencies(t *testing.T) {
	reg, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, m := range reg {
		pos[m.Module] = i
	}
	after := map[string][]string{
		regmig.Module:     {groups.Module},
		targetpage.Module: {groups.Module, participants.Module},
		letterread.Module: {letter.Module, draw.Module},
		audit.Module:      {groups.Module},
		AppModule:         {groups.Module, letterread.Module},
	}
	for mod, deps := range after {
		at, ok := pos[mod]
		if !ok {
			t.Fatalf("module %s absent du registre", mod)
		}
		for _, d := range deps {
			if pos[d] >= at {
				t.Errorf("%s (rang %d) doit suivre %s (rang %d)", mod, at, d, pos[d])
			}
		}
	}
	a := newApp(t)
	for _, table := range []string{
		"registration_invitations", "registration_pending", "registration_codes",
		"registration_outcomes", "registration_ignored", "registration_audit",
		"targetpage_budget_seen", "letterread_stamps", "admin_audit", "admin_link_handover", "removed_letter_archives",
	} {
		if n := countWhere(t, a, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table); n != 1 {
			t.Errorf("table %s absente", table)
		}
	}
	var versions int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE module = ?`, regmig.Module).Scan(&versions); err != nil || versions != 3 {
		t.Errorf("migrations de l'inscription appliquées : %d (%v), attendu 3", versions, err)
	}
}

func TestWave8ServicesBuilt(t *testing.T) {
	a := newApp(t)
	if a.Invite == nil || a.RegCodes == nil || a.Verify == nil || a.Adjust == nil || a.LetterRead == nil ||
		a.TargetPage == nil || a.AdminNav == nil || a.Audit == nil || a.Dashboard == nil {
		t.Fatalf("service manquant : %+v", a)
	}
	// Le compteur d'inscriptions en attente du tirage est celui de la vérification.
	if a.Draw == nil {
		t.Fatal("tirage absent")
	}
}

// --- Inscription par lien ---

var (
	codeInMailRe = regexp.MustCompile(`code de vérification : (\S+)`)
	tokenInMail  = regexp.MustCompile(`/inscription/confirmer/([A-Za-z0-9_-]+)`)
)

// Parcours complet : créer le groupe, activer l'invitation, s'inscrire, recevoir
// le code, le saisir, devenir participant (puis le lien en un clic).
func TestRegistrationJourneyCodeAndOneClickLink(t *testing.T) {
	a := wave8App(t)
	ctx := context.Background()
	createGroup(t, newBrowser(t, a), "noel", "Famille Noël")
	g, _ := a.Groups.BySlug(ctx, "noel")
	admin := participants.AdminActor(g.ID)
	link, err := a.Invite.Enable(ctx, admin, g.ID)
	if err != nil {
		t.Fatal(err)
	}

	visitor := newBrowser(t, a)
	// Le formulaire : sans effet, sans cache, sans référent, CSP stricte.
	page := visitor.get(link.Path)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="prenom"`) {
		t.Fatalf("formulaire : %d\n%s", page.Code, page.Body.String())
	}
	if page.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(page.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("en-têtes du formulaire : %v", page.Header())
	}
	if csp := page.Header().Get("Content-Security-Policy"); strings.Contains(csp, "unsafe-inline") || csp == "" {
		t.Errorf("CSP : %q", csp)
	}
	if inlineRe.MatchString(page.Body.String()) {
		t.Error("script ou style en ligne dans le formulaire")
	}
	if !strings.Contains(page.Body.String(), `name="site_web"`) {
		t.Error("champ piège absent")
	}

	// Réponse neutre identique pour slug inconnu, jeton faux, jeton absent.
	neutral := visitor.get("/noel/rejoindre/jeton-faux")
	for _, p := range []string{"/inconnu/rejoindre/x", "/noel/rejoindre", "/noel/rejoindre/"} {
		rec := visitor.get(p)
		if rec.Code != http.StatusNotFound || rec.Body.String() != neutral.Body.String() {
			t.Errorf("%s : %d, réponse différente de la réponse neutre", p, rec.Code)
		}
	}
	if neutral.Code != http.StatusNotFound {
		t.Fatalf("jeton faux : %d", neutral.Code)
	}

	// Champs invalides : 422 et message du catalogue.
	rec := visitor.post(link.Path, link.Path, url.Values{"prenom": {""}, "nom": {"Martin"}, "email": {"pas-une-adresse"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(rec.Body.String()), a.Cat.T("fr", "registration.first_name_required", nil)) ||
		!strings.Contains(html.UnescapeString(rec.Body.String()), a.Cat.T("fr", "registration.email_invalid", nil)) {
		t.Fatalf("saisie invalide : %d\n%s", rec.Code, rec.Body.String())
	}

	// Inscription valide : le code est envoyé, rien n'est encore un participant.
	rec = visitor.post(link.Path, link.Path, url.Values{"prenom": {"Léa"}, "nom": {"Martin"}, "email": {"lea@example.org"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="ref"`) || !strings.Contains(rec.Body.String(), `name="code"`) {
		t.Fatalf("inscription : %d\n%s", rec.Code, rec.Body.String())
	}
	ref := regexp.MustCompile(`name="ref" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())[1]
	if n, err := a.Verify.PendingCount(ctx, g.ID); err != nil || n != 1 {
		t.Fatalf("PendingCount = %d (%v), attendu 1", n, err)
	}
	if list, _ := a.Participants.List(ctx, admin, g.ID); len(list) != 0 {
		t.Fatalf("%d participants avant la vérification", len(list))
	}
	mail := queuedMail(t, a, "lea@example.org", "code_verification")
	code := codeInMailRe.FindStringSubmatch(mail)[1]

	// Un mauvais code est refusé sans rien créer.
	rec = visitor.post(link.Path, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {"ZZZZZZ"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(rec.Body.String()), a.Cat.T("fr", "registration.code.refused", nil)) {
		t.Fatalf("mauvais code : %d\n%s", rec.Code, rec.Body.String())
	}
	// Le bon code vérifie l'inscription : le participant existe, « bienvenue » part.
	rec = visitor.post(link.Path, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}})
	if rec.Code != http.StatusOK || !strings.Contains(html.UnescapeString(rec.Body.String()), a.Cat.T("fr", "registration.public.done.title", nil)) {
		t.Fatalf("bon code : %d\n%s", rec.Code, rec.Body.String())
	}
	list, _ := a.Participants.List(ctx, admin, g.ID)
	if len(list) != 1 || list[0].FirstName != "Léa" || list[0].Email != "lea@example.org" {
		t.Fatalf("participants : %+v", list)
	}
	if !strings.Contains(queuedMail(t, a, "lea@example.org", "bienvenue"), "/noel/acces/") {
		t.Error("le courriel de bienvenue doit porter le lien personnel")
	}
	if n, _ := a.Verify.PendingCount(ctx, g.ID); n != 0 {
		t.Errorf("PendingCount après vérification = %d", n)
	}
	// Idempotent : le même code ne crée pas de second participant.
	visitor.post(link.Path, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}})
	if list, _ := a.Participants.List(ctx, admin, g.ID); len(list) != 1 {
		t.Errorf("%d participants après un second envoi du code", len(list))
	}

	// Lien en un clic : le GET ne change rien, le POST vérifie, une seule fois.
	rec = visitor.post(link.Path, link.Path, url.Values{"prenom": {"Hugo"}, "nom": {"Bernard"}, "email": {"hugo@example.org"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("seconde inscription : %d", rec.Code)
	}
	mail = queuedMail(t, a, "hugo@example.org", "code_verification")
	confirm := "/noel/inscription/confirmer/" + tokenInMail.FindStringSubmatch(mail)[1]
	rec = visitor.get(confirm)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), codeInMailRe.FindStringSubmatch(mail)[1]) {
		t.Fatalf("page de confirmation : %d\n%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("le lien de confirmation doit porter Referrer-Policy: no-referrer")
	}
	if list, _ := a.Participants.List(ctx, admin, g.ID); len(list) != 1 {
		t.Fatalf("le GET de confirmation a créé un participant : %d", len(list))
	}
	if r := visitor.do(http.MethodPost, confirm, url.Values{}); r.Code != http.StatusForbidden {
		t.Errorf("POST sans jeton CSRF : %d, attendu 403", r.Code)
	}
	if rec = visitor.post(confirm, confirm, url.Values{}); rec.Code != http.StatusOK || !strings.Contains(html.UnescapeString(rec.Body.String()), a.Cat.T("fr", "registration.public.done.title", nil)) {
		t.Fatalf("confirmation : %d\n%s", rec.Code, rec.Body.String())
	}
	if list, _ := a.Participants.List(ctx, admin, g.ID); len(list) != 2 {
		t.Fatalf("%d participants après la confirmation, attendu 2", len(list))
	}
	if rec = visitor.get(confirm); rec.Code != http.StatusNotFound {
		t.Errorf("lien déjà utilisé : %d, attendu 404", rec.Code)
	}
	if rec = visitor.get("/noel/inscription/confirmer/jeton-faux"); rec.Code != http.StatusNotFound {
		t.Errorf("jeton faux : %d, attendu 404", rec.Code)
	}
}

// Le tirage refuse tant qu'une inscription est en attente (compteur réel, plus
// l'adaptateur vide) ; « ignorer » la ferme, et après le tirage plus rien ne se
// vérifie. La purge à 48 heures retire tout, y compris l'état « ignorée ».
func TestRegistrationPendingLocksDrawIgnoreClosesAndPurges(t *testing.T) {
	a := wave8App(t)
	ctx := context.Background()
	createGroup(t, newBrowser(t, a), "noel", "Famille Noël")
	g, _ := a.Groups.BySlug(ctx, "noel")
	admin := participants.AdminActor(g.ID)
	for _, p := range []participants.Input{
		{FirstName: "Léa", Name: "Martin", Email: "lea@example.org"},
		{FirstName: "Hugo", Name: "Bernard", Email: "hugo@example.org"},
		{FirstName: "Inès", Name: "Moreau", Email: "ines@example.org"},
	} {
		if _, err := a.Participants.Create(ctx, admin, g.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	link, _ := a.Invite.Enable(ctx, admin, g.ID)
	visitor := newBrowser(t, a)
	rec := visitor.post(link.Path, link.Path, url.Values{"prenom": {"Zoé"}, "nom": {"Petit"}, "email": {"zoe@example.org"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("inscription : %d", rec.Code)
	}
	ref := regexp.MustCompile(`name="ref" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())[1]
	code := codeInMailRe.FindStringSubmatch(queuedMail(t, a, "zoe@example.org", "code_verification"))[1]

	_, err := a.Drawing.Draw(ctx, admin, g.ID, draw.Request{Confirmed: true})
	if c, _ := errs.CodeOf(err); c != draw.CodePendingRegistrations {
		t.Fatalf("tirage avec une inscription en attente : %v, attendu %s", err, draw.CodePendingRegistrations)
	}
	if err := a.Draw.ResolvePending(ctx, admin, g.ID, ports.ResolveIgnore); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM registration_ignored WHERE group_id = ?`, g.ID); n != 1 {
		t.Fatalf("inscriptions ignorées en base : %d", n)
	}
	if n, _ := a.Verify.PendingCount(ctx, g.ID); n != 0 {
		t.Fatalf("PendingCount hors ignorées = %d", n)
	}
	if _, err := a.Drawing.Draw(ctx, admin, g.ID, draw.Request{Confirmed: true}); err != nil {
		t.Fatalf("tirage : %v", err)
	}

	// Après le tirage : formulaire clos, code refusé (le tirage est fait), rien créé.
	closed := a.Cat.T("fr", "registration.closed_draw", nil)
	if rec = visitor.get(link.Path); rec.Code != http.StatusOK || !strings.Contains(html.UnescapeString(rec.Body.String()), closed) {
		t.Errorf("formulaire après le tirage : %d", rec.Code)
	}
	rec = visitor.post(link.Path, link.Path, url.Values{"prenom": {"Noé"}, "nom": {"Roux"}, "email": {"noe@example.org"}})
	if !strings.Contains(html.UnescapeString(rec.Body.String()), closed) {
		t.Errorf("inscription après le tirage : %d\n%s", rec.Code, rec.Body.String())
	}
	rec = visitor.post(link.Path, "/noel/inscription/code", url.Values{"ref": {ref}, "code": {code}})
	if rec.Code != http.StatusForbidden || !strings.Contains(html.UnescapeString(rec.Body.String()), closed) {
		t.Errorf("code après le tirage : %d\n%s", rec.Code, rec.Body.String())
	}
	if list, _ := a.Participants.List(ctx, admin, g.ID); len(list) != 3 {
		t.Errorf("%d participants, attendu 3", len(list))
	}

	// Purge à 48 heures par la tâche planifiée à l'émission du code.
	a.Clock.(*clock.Fake).Advance(49 * time.Hour)
	if _, err := a.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"registration_pending", "registration_codes", "registration_ignored"} {
		if n := countWhere(t, a, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Errorf("%s : %d lignes après la purge à 48 heures", table, n)
		}
	}
}

// Au démarrage, la purge reprend sans tâche en mémoire : les inscriptions
// expirées partent (ListPending à l'instant zéro les voit), les vivantes
// reçoivent une nouvelle tâche à leur expiration.
func TestRegistrationPurgeRestoredAtStart(t *testing.T) {
	a := wave8App(t)
	ctx := context.Background()
	createGroup(t, newBrowser(t, a), "noel", "Famille Noël")
	g, _ := a.Groups.BySlug(ctx, "noel")
	link, _ := a.Invite.Enable(ctx, participants.AdminActor(g.ID), g.ID)
	visitor := newBrowser(t, a)
	visitor.post(link.Path, link.Path, url.Values{"prenom": {"Léa"}, "nom": {"Martin"}, "email": {"lea@example.org"}})
	fake := a.Clock.(*clock.Fake)
	fake.Advance(30 * time.Hour)
	visitor.post(link.Path, link.Path, url.Values{"prenom": {"Hugo"}, "nom": {"Bernard"}, "email": {"hugo@example.org"}})
	fake.Advance(19 * time.Hour) // la première a 49 heures, la seconde 19
	before := a.sched.Pending()
	if err := a.restoreRegistrations(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM registration_pending`); n != 1 {
		t.Fatalf("inscriptions après la reprise : %d, attendu 1 (la vivante)", n)
	}
	if after := a.sched.Pending(); after != before+1 {
		t.Errorf("tâches planifiées %d -> %d : une purge à l'expiration de la vivante est attendue", before, after)
	}
	fake.Advance(30 * time.Hour)
	if _, err := a.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM registration_pending`); n != 0 {
		t.Errorf("inscriptions après l'expiration de la dernière : %d", n)
	}
}

// --- Pages du participant et lecture de la lettre ---

var targetIDRe = regexp.MustCompile(`"id":(\d+)`)

func pairsOf(t *testing.T, a *App) [][2]int64 {
	t.Helper()
	rows, err := a.DB.Query(`SELECT p.giver_id, p.receiver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.status = 'actuel' ORDER BY p.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]int64
	for rows.Next() {
		var p [2]int64
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		img.Set(x, x, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// /{slug}/participant ne renvoie plus 404 pour une session participant : attente
// avant le tirage, renvoi vers la découverte, puis carte de cible ; le budget
// mis à jour est signalé jusqu'à ce qu'il soit vu ; la préférence « un seul
// cadeau » passe par POST seulement. La lettre de la cible se lit (image) seulement
// après la découverte, et seulement par son tireur.
func TestParticipantPagesTargetCardBudgetAndLetterImage(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	ctx := context.Background()
	slug := "/" + revealSlug

	// Sans session : renvoi vers la connexion (plus de 404) ; groupe inconnu : 404.
	anon := newBrowser(t, a)
	for _, p := range []string{"/participant", "/ma-cible"} {
		if rec := anon.get(slug + p); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != slug+"/connexion" {
			t.Errorf("%s sans session : %d %q", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := anon.get("/inconnu/participant"); rec.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", rec.Code)
	}

	// Avant le tirage : attente ; la préférence passe par POST.
	other := e.login(1)
	rec := other.get(slug + "/participant")
	if rec.Code != http.StatusOK || !strings.Contains(html.UnescapeString(rec.Body.String()), a.Cat.T("fr", "draw.wait.title.notdone", nil)) {
		t.Fatalf("attente : %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := other.get(slug + "/preference"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /preference : %d, attendu 405", rec.Code)
	}
	if rec := other.do(http.MethodPost, slug+"/preference", url.Values{"un_seul": {"1"}}); rec.Code != http.StatusForbidden {
		t.Errorf("POST /preference sans jeton CSRF : %d, attendu 403", rec.Code)
	}
	if rec := other.post(slug+"/participant", slug+"/preference", url.Values{"un_seul": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /preference : %d\n%s", rec.Code, rec.Body.String())
	}
	if p, err := a.Participants.Get(ctx, e.admin, e.gid, e.ids[1]); err != nil || p.Role != participants.RoleUnSeul {
		t.Errorf("rôle après la préférence : %v (%v)", p, err)
	}

	// Un budget défini avant le tirage : version 1.
	if _, err := a.GroupSettings.UpdateGroupSettings(ctx, groupsettings.Input{GroupID: e.gid, Name: "Famille Martin", Budget: "25"}); err != nil {
		t.Fatal(err)
	}
	if g, _ := a.Groups.ByID(ctx, e.gid); g.BudgetVersion != 1 {
		t.Fatalf("budget_version = %d, attendu 1", g.BudgetVersion)
	}
	e.drawNow()

	b := e.login(0)
	if rec := b.get(slug + "/participant"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != slug+"/decouverte" {
		t.Fatalf("cible ouverte mais non découverte : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	pairs := pairsOf(t, a)
	var receiver int64
	for _, p := range pairs {
		if p[0] == e.ids[0] {
			receiver = p[1]
		}
	}
	idx := -1
	for i, id := range e.ids {
		if id == receiver {
			idx = i
		}
	}
	// La lettre de la cible, avec une image, écrite avant la lecture.
	if _, err := a.Letter.SetIntro(ctx, e.gid, receiver, "Je rêve d'un vélo"); err != nil {
		t.Fatal(err)
	}
	ideaID, err := a.Letter.AddIdea(ctx, e.gid, receiver, letter.IdeaInput{Title: "Vélo rouge", Image: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	pairID := strconv.Itoa(int(pairIDOf(t, a, e.ids[0], receiver)))
	imagePath := slug + "/lettre-recue/" + pairID + "/image/" + strconv.FormatInt(ideaID, 10)

	// Avant la découverte : la lettre est introuvable (404 neutre).
	if rec := b.get(imagePath); rec.Code != http.StatusNotFound {
		t.Errorf("image avant la découverte : %d, attendu 404", rec.Code)
	}
	page := b.get(slug + "/decouverte")
	token := html.UnescapeString(dataCSRFRe.FindStringSubmatch(page.Body.String())[1])
	_, body := targetsOf(t, b)
	if m := targetIDRe.FindStringSubmatch(body); m == nil || m[1] != pairID {
		t.Fatalf("cibles : %s (paire attendue %s)", body, pairID)
	}
	if rec := b.postJSON(slug+"/decouverte/confirmation", token, `{"target_id":`+pairID+`}`); rec.Code != http.StatusOK {
		t.Fatalf("découverte : %d %s", rec.Code, rec.Body.String())
	}

	// Carte de cible : le prénom de la cible seulement, le bandeau de budget.
	for _, p := range []string{"/participant", "/ma-cible"} {
		rec := b.get(slug + p)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s après la découverte : %d\n%s", p, rec.Code, rec.Body.String())
		}
		first := strings.Fields(e.names[idx])[0]
		if !strings.Contains(rec.Body.String(), first) {
			t.Errorf("%s : le prénom de la cible %q est absent", p, first)
		}
		for i, n := range e.names {
			if i != idx && i != 0 && strings.Contains(rec.Body.String(), strings.Fields(n)[0]) {
				t.Errorf("%s : le prénom %q d'une autre personne est affiché", p, n)
			}
		}
		if inlineRe.MatchString(rec.Body.String()) || strings.Contains(rec.Header().Get("Content-Security-Policy"), "unsafe-inline") {
			t.Errorf("%s : script ou style en ligne, ou CSP laxiste", p)
		}
	}
	if !strings.Contains(b.get(slug+"/participant").Body.String(), `draw-banner`) {
		t.Error("le bandeau « budget mis à jour » est attendu")
	}
	if rec := b.post(slug+"/participant", slug+"/budget-vu", url.Values{"version": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("budget-vu : %d", rec.Code)
	}
	if strings.Contains(b.get(slug+"/participant").Body.String(), `draw-banner`) {
		t.Error("le bandeau doit disparaître une fois le budget vu")
	}
	// Un nouveau changement de budget (version 2) le fait revenir.
	if _, err := a.GroupSettings.UpdateGroupSettings(ctx, groupsettings.Input{GroupID: e.gid, Name: "Famille Martin", Budget: "30"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.get(slug+"/participant").Body.String(), `draw-banner`) {
		t.Error("un budget modifié doit réafficher le bandeau")
	}

	// L'image de la lettre : lecture par le tireur seulement.
	rec = b.get(imagePath)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/") || rec.Body.Len() == 0 {
		t.Fatalf("image : %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("Cache-Control de l'image : %q", rec.Header().Get("Cache-Control"))
	}
	if rec := anon.get(imagePath); rec.Code != http.StatusNotFound {
		t.Errorf("image sans session : %d", rec.Code)
	}
	for i := range e.ids {
		if e.ids[i] == e.ids[0] {
			continue
		}
		if rec := e.login(i).get(imagePath); rec.Code != http.StatusNotFound {
			t.Errorf("image lue par la personne %d (pas le tireur) : %d, attendu 404", i, rec.Code)
		}
	}
	if rec := b.do(http.MethodPost, imagePath, url.Values{}); rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusForbidden {
		t.Errorf("POST sur la route de lecture : %d (aucune écriture pour le tireur)", rec.Code)
	}
	// La lecture se voit côté tiré (cachet) : la route de lecture est branchée
	// sur le service de letterread.
	if _, err := a.LetterRead.Read(ctx, participants.ParticipantActor(e.gid, e.ids[0]), e.gid, pairIDOf(t, a, e.ids[0], receiver)); err != nil {
		t.Fatal(err)
	}
	st, err := a.LetterRead.Stamp(ctx, participants.ParticipantActor(e.gid, receiver), e.gid)
	if err != nil || !st.Read {
		t.Errorf("cachet : %+v (%v)", st, err)
	}
}

func pairIDOf(t *testing.T, a *App, giver, receiver int64) int64 {
	t.Helper()
	var id int64
	if err := a.DB.QueryRow(`SELECT p.id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.status = 'actuel' AND p.giver_id = ? AND p.receiver_id = ?`, giver, receiver).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// --- Administration ---

// Chaque page d'administration exige la session administrateur de SON groupe,
// porte son propre contenu (un bloc layout/content par page), des liens de
// navigation sans {slug}, une CSP stricte, et /{slug}/admin ne heurte pas
// /{slug}/admin/connexion.
func TestAdminPagesRequireOwnGroupSessionAndRenderOwnContent(t *testing.T) {
	a := newApp(t)
	creator := newBrowser(t, a)
	secret := createGroup(t, creator, "famille", "Famille Martin")
	createGroup(t, newBrowser(t, a), "amis", "Les amis")
	admin := adminLogin(t, a, "famille", secret)

	pages := []struct {
		path, titleKey, nav string
		status              int
	}{
		{"/famille/admin", "admin.dashboard.title", "/famille/admin", http.StatusOK},
		// Le contrôle du tirage répond 409 tant que le tirage n'est pas fait :
		// la page est rendue avec son message.
		{"/famille/admin/controle", "admin.dashboard.check.title", "/famille/admin", http.StatusConflict},
		{"/famille/admin/journal", "admin.dashboard.journal.title", "/famille/admin", http.StatusOK},
		{"/famille/admin/plus", "admin.menu.title", "/famille/admin/plus", http.StatusOK},
		{"/famille/admin/roles", "admin.roles.title", "/famille/admin/tirage", http.StatusOK},
	}
	for _, p := range pages {
		anon := newBrowser(t, a)
		if rec := anon.get(p.path); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/famille/admin/connexion" {
			t.Errorf("%s sans session : %d %q", p.path, rec.Code, rec.Header().Get("Location"))
		}
		// La session d'un autre groupe ne vaut rien ici.
		if rec := admin.get(strings.Replace(p.path, "/famille/", "/amis/", 1)); rec.Code != http.StatusSeeOther ||
			rec.Header().Get("Location") != "/amis/admin/connexion" {
			t.Errorf("%s avec la session d'un autre groupe : %d %q", p.path, rec.Code, rec.Header().Get("Location"))
		}
		rec := admin.get(p.path)
		if rec.Code != p.status {
			t.Fatalf("%s : %d, attendu %d\n%s", p.path, rec.Code, p.status, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, a.Cat.T("fr", p.titleKey, nil)) {
			t.Errorf("%s : titre %q absent (contenu d'une autre page ?)", p.path, p.titleKey)
		}
		if strings.Contains(body, "{slug}") || strings.Contains(body, "%7Bslug%7D") || strings.Contains(body, "%7bslug%7d") {
			t.Errorf("%s : {slug} non résolu dans la navigation", p.path)
		}
		if !strings.Contains(body, `href="`+p.nav+`"`) {
			t.Errorf("%s : lien de navigation %s absent", p.path, p.nav)
		}
		if n := strings.Count(body, `aria-current="page"`); n != 1 {
			t.Errorf("%s : %d entrées actives, attendu 1", p.path, n)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); csp == "" || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s : CSP %q", p.path, csp)
		}
		if inlineRe.MatchString(body) {
			t.Errorf("%s : script ou style en ligne", p.path)
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("%s : Cache-Control %q", p.path, rec.Header().Get("Cache-Control"))
		}
	}
	if rec := newBrowser(t, a).get("/famille/admin/connexion"); rec.Code != http.StatusOK {
		t.Errorf("/famille/admin/connexion : %d, doit rester la page de connexion", rec.Code)
	}
	if rec := admin.get("/inconnu/admin"); rec.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", rec.Code)
	}
	// Les entrées du registre : Tableau, Participants, Tirage, Inscriptions, Plus.
	if n := strings.Count(admin.get("/famille/admin").Body.String(), `<a class="admin-nav-item"`); n != 5 {
		t.Errorf("%d entrées de navigation, attendu 5", n)
	}
}

// Actions d'administration : « lien remis » (journal), rôles (journal), CSRF
// exigé, déconnexion.
func TestAdminActionsJournalRolesAndLogout(t *testing.T) {
	a := newApp(t)
	secret := createGroup(t, newBrowser(t, a), "famille", "Famille Martin")
	g, _ := a.Groups.BySlug(context.Background(), "famille")
	adminActor := participants.AdminActor(g.ID)
	noMail, err := a.Participants.Create(context.Background(), adminActor, g.ID, participants.Input{FirstName: "Paul", Name: "Garnier"})
	if err != nil {
		t.Fatal(err)
	}
	withMail, err := a.Participants.Create(context.Background(), adminActor, g.ID, participants.Input{FirstName: "Léa", Name: "Dupuis", Email: "lea@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	b := adminLogin(t, a, "famille", secret)

	// Lien remis : refusé sans jeton CSRF, accepté avec ; consigné au journal.
	handover := "/famille/admin/personnes/" + strconv.FormatInt(noMail.ID, 10) + "/lien-remis"
	if rec := b.do(http.MethodPost, handover, url.Values{}); rec.Code != http.StatusForbidden {
		t.Errorf("lien remis sans jeton CSRF : %d, attendu 403", rec.Code)
	}
	if rec := b.post("/famille/admin", handover, url.Values{}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/famille/admin?remis=1" {
		t.Fatalf("lien remis : %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM admin_audit WHERE group_id = ? AND action = 'lien_remis' AND participant_id = ?`, g.ID, noMail.ID); n != 1 {
		t.Errorf("lignes du journal : %d, attendu 1", n)
	}
	// Une personne avec courriel n'a pas de lien à remettre.
	if rec := b.post("/famille/admin", "/famille/admin/personnes/"+strconv.FormatInt(withMail.ID, 10)+"/lien-remis", url.Values{}); rec.Code == http.StatusSeeOther {
		t.Errorf("lien remis pour une personne avec courriel : %d", rec.Code)
	}

	// Rôles : changement direct avant le tirage, consigné au journal.
	rec := b.post("/famille/admin/roles", "/famille/admin/roles", url.Values{"ids": {strconv.FormatInt(withMail.ID, 10)}, "role": {"reception_seule"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rôles : %d\n%s", rec.Code, rec.Body.String())
	}
	if p, _ := a.Participants.Get(context.Background(), adminActor, g.ID, withMail.ID); p.Role != participants.RoleReceptionSeule {
		t.Errorf("rôle : %s", p.Role)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM admin_audit WHERE group_id = ? AND action = 'roles_changement'`, g.ID); n != 1 {
		t.Errorf("changements de rôles au journal : %d, attendu 1", n)
	}
	journal := b.get("/famille/admin/journal").Body.String()
	for _, key := range []string{"admin.dashboard.action.lien_remis", "admin.dashboard.action.roles_changement"} {
		if !strings.Contains(journal, a.Cat.T("fr", key, nil)) {
			t.Errorf("journal : %q absent", key)
		}
	}

	// Déconnexion : POST seulement, avec jeton ; la session disparaît.
	if rec := b.do(http.MethodPost, "/famille/admin/deconnexion", url.Values{}); rec.Code != http.StatusForbidden {
		t.Errorf("déconnexion sans jeton CSRF : %d, attendu 403", rec.Code)
	}
	if rec := b.get("/famille/admin/deconnexion"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /deconnexion : %d, attendu 405", rec.Code)
	}
	if rec := b.post("/famille/admin/plus", "/famille/admin/deconnexion", url.Values{}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/famille/admin/connexion" {
		t.Fatalf("déconnexion : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := b.get("/famille/admin"); rec.Code != http.StatusSeeOther {
		t.Errorf("tableau de bord après la déconnexion : %d", rec.Code)
	}
}

// --- Ajustement du tirage ---

// Retirer une personne : l'archive de sa lettre survit à sa suppression, ses
// accès sont révoqués, seules les personnes concernées sont prévenues (sans
// nom), la révélation suit.
func TestAdjustRemoveKeepsLetterArchiveAndRevokesAccess(t *testing.T) {
	a := wave8App(t)
	e := newRevealEnv(t, a)
	ctx := context.Background()
	e.drawNow()
	removed := e.ids[3]
	if _, err := a.Letter.SetIntro(ctx, e.gid, removed, "Je rêve d'un vélo"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Letter.AddIdea(ctx, e.gid, removed, letter.IdeaInput{Title: "Vélo rouge"}); err != nil {
		t.Fatal(err)
	}
	// Accès de la personne retirée : session, lien personnel, code, lien magique.
	e.login(3)
	gs, ps := strconv.FormatInt(e.gid, 10), strconv.FormatInt(removed, 10)
	if _, err := a.Tokens.IssueLink(ctx, gs, ps); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Tokens.IssueCode(ctx, gs, ps); err != nil {
		t.Fatal(err)
	}
	access := func() int {
		return countWhere(t, a, `SELECT (SELECT COUNT(*) FROM websec_sessions WHERE kind = 'participant' AND subject = ?)
			+ (SELECT COUNT(*) FROM access_credentials WHERE participant_id = ?)
			+ (SELECT COUNT(*) FROM magic_links WHERE participant_id = ?)`, ps, ps, removed)
	}
	if access() < 4 {
		t.Fatalf("accès de la personne avant le retrait : %d, attendu au moins 4", access())
	}

	change := adjust.Change{Kind: adjust.KindRemove, ParticipantID: removed}
	prev, err := a.Adjust.Preview(ctx, e.admin, e.gid, change)
	if err != nil || !prev.Possible {
		t.Fatalf("aperçu : %+v (%v)", prev, err)
	}
	res, err := a.Adjust.Apply(ctx, e.admin, e.gid, change, adjust.Confirmation{Confirmed: true, Revision: prev.Revision})
	if err != nil {
		t.Fatalf("ajustement : %v", err)
	}

	// La personne est retirée, ses accès aussi, les autres restent.
	if _, err := a.Participants.Get(ctx, e.admin, e.gid, removed); !errs.Is(err, errs.KindNotFound) {
		t.Errorf("la personne retirée existe encore : %v", err)
	}
	if n := access(); n != 0 {
		t.Errorf("%d accès subsistent après le retrait", n)
	}
	if list, _ := a.Participants.List(ctx, e.admin, e.gid); len(list) != 3 {
		t.Errorf("%d participants, attendu 3", len(list))
	}
	for _, p := range pairsOf(t, a) {
		if p[0] == removed || p[1] == removed {
			t.Errorf("paire restante avec la personne retirée : %v", p)
		}
	}

	// L'archive survit : copiée avant la suppression, hors cascade.
	if n := countWhere(t, a, `SELECT COUNT(*) FROM letters WHERE participant_id = ?`, removed); n != 0 {
		t.Errorf("la lettre de la personne retirée existe encore : %d", n)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM letter_archives`); n != 0 {
		t.Errorf("letter_archives garde %d lignes d'une lettre supprimée (cascade)", n)
	}
	var snap string
	if err := a.DB.QueryRow(`SELECT snapshot FROM removed_letter_archives WHERE group_id = ? AND participant_id = ?`, e.gid, removed).Scan(&snap); err != nil {
		t.Fatalf("archive de la lettre retirée : %v", err)
	}
	if !strings.Contains(snap, "Vélo rouge") || !strings.Contains(snap, "Je rêve d'un vélo") {
		t.Errorf("instantané incomplet : %s", snap)
	}
	if strings.Contains(snap, "giver") || strings.Contains(snap, "tireur") {
		t.Errorf("l'instantané ne doit pas désigner le tireur : %s", snap)
	}

	// Avis : seulement aux personnes concernées, sans aucun nom.
	if res.Notified < 1 || res.NotifyFailed != 0 {
		t.Fatalf("avis : %+v", res)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM mail_queue WHERE mail_type = 'draw_adjusted'`); n != res.Notified {
		t.Errorf("courriels d'ajustement en file : %d, attendu %d", n, res.Notified)
	}
	rows, err := a.DB.Query(`SELECT recipient, body_text FROM mail_queue WHERE mail_type = 'draw_adjusted'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var to, body string
		if err := rows.Scan(&to, &body); err != nil {
			t.Fatal(err)
		}
		if to == "paul@example.org" {
			t.Error("la personne retirée ne doit pas être prévenue")
		}
		if !strings.Contains(body, "/participant") {
			t.Errorf("le courriel doit mener à /participant : %s", body)
		}
		for i, n := range e.names {
			if i != 3 && strings.Contains(body, strings.Fields(n)[1]) {
				t.Errorf("le courriel à %s nomme %q", to, n)
			}
		}
	}

	// La révélation suit : toute paire actuelle est ouverte.
	if n := countWhere(t, a, `SELECT COUNT(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.status = 'actuel' AND NOT EXISTS (SELECT 1 FROM reveal_targets t WHERE t.pair_id = p.id)`); n != 0 {
		t.Errorf("%d paires actuelles sans ouverture de révélation", n)
	}
}

// Seul l'administrateur du groupe ajuste ; sans confirmation rien ne change.
func TestAdjustRequiresAdminAndConfirmation(t *testing.T) {
	a := wave8App(t)
	e := newRevealEnv(t, a)
	ctx := context.Background()
	e.drawNow()
	change := adjust.Change{Kind: adjust.KindRemove, ParticipantID: e.ids[3]}
	if _, err := a.Adjust.Preview(ctx, participants.ParticipantActor(e.gid, e.ids[0]), e.gid, change); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("aperçu par un participant : %v", err)
	}
	if _, err := a.Adjust.Apply(ctx, participants.AdminActor(e.gid+1), e.gid, change, adjust.Confirmation{Confirmed: true}); !errs.Is(err, errs.KindForbidden) {
		t.Errorf("ajustement par l'administrateur d'un autre groupe : %v", err)
	}
	if _, err := a.Adjust.Apply(ctx, e.admin, e.gid, change, adjust.Confirmation{}); !errs.Is(err, errs.KindValidation) {
		t.Errorf("ajustement sans confirmation : %v", err)
	}
	if list, _ := a.Participants.List(ctx, e.admin, e.gid); len(list) != 4 {
		t.Errorf("%d participants, attendu 4", len(list))
	}
}
