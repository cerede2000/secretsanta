package app

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/maildiag"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminders"
	"secretsanta/internal/shared/clock"
)

// rawKeyRe repère une clé de texte non traduite (suggestions.error.title...).
var rawKeyRe = regexp.MustCompile(`\bsuggestions\.[a-z_]+\.[a-z_]+`)

// noDNS est un résolveur sans réseau : tout enregistrement est absent.
type noDNS struct{}

func (noDNS) LookupTXT(context.Context, string) ([]string, error) { return nil, maildiag.ErrNotFound }
func (noDNS) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, maildiag.ErrNotFound }

// withoutDNS rend le diagnostic de délivrabilité hermétique le temps du test.
func withoutDNS(t *testing.T) {
	t.Helper()
	old := mailDiagOptions
	mailDiagOptions = func(o *maildiag.Options) { o.Resolver = noDNS{}; o.Timeout = 200 * time.Millisecond }
	t.Cleanup(func() { mailDiagOptions = old })
}

// La page « Délivrabilité » est montée, réservée à l'administrateur du groupe,
// atteinte depuis la feuille « Plus » ; ses écritures sont en POST seulement ;
// le courriel de test part dans la langue de la page (Content-Language).
func TestWave11MailDiagPage(t *testing.T) {
	withoutDNS(t)
	e := newW9(t)
	const page = "/noel/admin/courrier/delivrabilite"
	anon := newBrowser(t, e.a)
	if rec := anon.get(page); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
		t.Fatalf("sans session : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec := e.admin.get(page)
	if rec.Code != http.StatusOK {
		t.Fatalf("page : %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "maildiag.check") || strings.Contains(body, "maildiag.title") {
		t.Errorf("clé de texte brute dans la page :\n%s", body)
	}
	plus := e.admin.get("/noel/admin/plus").Body.String()
	if !strings.Contains(plus, `href="`+page+`"`) {
		t.Errorf("la feuille Plus ne mène pas à la page de délivrabilité :\n%s", plus)
	}
	if strings.Contains(plus, "admin.menu.maildiag") {
		t.Error("clé de texte brute dans la feuille Plus")
	}
	// Une écriture ne se fait jamais en GET.
	for _, p := range []string{page + "/test", page + "/suppression/retirer"} {
		if rec := e.admin.get(p); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s : %d, 405 attendu", p, rec.Code)
		}
	}
	// Courriel de test : adresse invalide refusée, adresse valide mise en file.
	if rec := e.admin.post(page, page+"/test", url.Values{"to": {"pas-une-adresse"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("adresse invalide : %d", rec.Code)
	}
	if rec := e.admin.post(page, page+"/test", url.Values{"to": {"essai@example.org"}}); rec.Code != http.StatusOK {
		t.Fatalf("courriel de test : %d\n%s", rec.Code, rec.Body.String())
	}
	var lang, state string
	if err := e.a.DB.QueryRow(`SELECT lang, status FROM mail_queue WHERE recipient = 'essai@example.org' AND mail_type = 'test'`).Scan(&lang, &state); err != nil {
		t.Fatalf("courriel de test absent de la file : %v", err)
	}
	if lang != "fr" || state != "sent" {
		t.Errorf("courriel de test : langue %q (fr attendu), état %q (sent attendu)", lang, state)
	}
	// Liste de suppression : retirer une adresse refusée.
	if err := e.a.Suppress.Add(context.Background(), e.gid, "refusee@example.org", "5.1.1"); err != nil {
		t.Fatal(err)
	}
	if body := e.admin.get(page).Body.String(); !strings.Contains(body, "refusee@example.org") {
		t.Error("adresse refusée absente de la liste de suppression")
	}
	if rec := e.admin.post(page, page+"/suppression/retirer", url.Values{"address": {"refusee@example.org"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("retrait : %d", rec.Code)
	}
	if sup, _ := e.a.Suppress.IsSuppressed(context.Background(), e.gid, "refusee@example.org"); sup {
		t.Error("adresse toujours supprimée après le retrait")
	}
}

// Sans adresse d'expédition, la page reste lisible : le courriel de test est
// refusé net, sans en-têtes ni panique.
func TestWave11MailDiagPageWithoutMail(t *testing.T) {
	withoutDNS(t)
	a := newApp(t)
	secret := createGroup(t, newBrowser(t, a), "noel", "Famille Noël")
	admin := adminLogin(t, a, "noel", secret)
	const page = "/noel/admin/courrier/delivrabilite"
	if rec := admin.get(page); rec.Code != http.StatusOK {
		t.Fatalf("page : %d", rec.Code)
	}
	rec := admin.post(page, page+"/test", url.Values{"to": {"essai@example.org"}})
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Message-ID") {
		t.Fatalf("courriel de test sans envoi : %d", rec.Code)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM mail_queue`); n != 0 {
		t.Errorf("%d message(s) en file sans transport", n)
	}
}

// ouvrir la révélation : un tirage sans date de révélation ouvre aussitôt ;
// renvoie la personne i connectée et la paire (cible) qu'elle a découverte.
func (e *revealEnv) discovered(i int) (*browser, string) {
	e.t.Helper()
	b := e.login(i)
	page := b.get("/" + revealSlug + "/decouverte")
	token := dataCSRFValue(e.t, page.Body.String())
	_, body := targetsOf(e.t, b)
	m := targetIDRe.FindStringSubmatch(body)
	if m == nil {
		e.t.Fatalf("aucune cible lue : %s", body)
	}
	if rec := b.postJSON("/"+revealSlug+"/decouverte/confirmation", token, `{"target_id":`+m[1]+`}`); rec.Code != http.StatusOK {
		e.t.Fatalf("découverte : %d", rec.Code)
	}
	return b, m[1]
}

func dataCSRFValue(t *testing.T, body string) string {
	t.Helper()
	m := dataCSRFRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("jeton CSRF absent : %s", body)
	}
	return m[1]
}

// La page des idées du tireur est montée sur la route que la page de la cible
// désigne : réservée à la personne connectée et à ses cibles découvertes, avec
// l'actualisation en POST seulement ; la page de révélation charge la séquence
// de deux cadeaux (bouton « suivant », pastille, trois styles).
func TestWave11SuggestionPageAndRevealSequence(t *testing.T) {
	a := wave8App(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	b, pair := e.discovered(0)

	reveal := b.get("/" + revealSlug + "/decouverte").Body.String()
	for _, want := range []string{
		`data-role="next"`, `data-role="pill"`, "data-t-pill=", "/static/js/reveal/core/main.js",
		"/static/js/reveal/gift/gift.js", "/static/js/reveal/envelope/envelope.js", "/static/js/reveal/scratch/scratch.js",
	} {
		if !strings.Contains(reveal, want) {
			t.Errorf("la page de révélation ne contient pas %q", want)
		}
	}

	ideas := "/" + revealSlug + "/suggestions/" + pair
	if !strings.Contains(b.get("/"+revealSlug+"/ma-cible").Body.String(), `href="`+ideas+`"`) {
		t.Errorf("la page de la cible ne lie pas %s", ideas)
	}
	if rec := newBrowser(t, a).get(ideas); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/"+revealSlug+"/connexion" {
		t.Errorf("sans session : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec := b.get(ideas)
	if rec.Code != http.StatusOK {
		t.Fatalf("page des idées : %d\n%s", rec.Code, rec.Body.String())
	}
	if rawKeyRe.MatchString(rec.Body.String()) {
		t.Errorf("clé de texte brute dans la page :\n%s", rec.Body.String())
	}
	if rec := b.get("/" + revealSlug + "/suggestions/999999"); rec.Code != http.StatusNotFound {
		t.Errorf("paire d'autrui : %d, 404 attendu", rec.Code)
	}
	if rec := b.get(ideas + "/actualiser"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET sur l'actualisation : %d, 405 attendu", rec.Code)
	}
	if rec := b.post(ideas, ideas+"/actualiser", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("actualisation : %d", rec.Code)
	}
	// Une autre personne n'a pas cette paire.
	if rec := e.login(1).get(ideas); rec.Code != http.StatusNotFound {
		t.Errorf("paire d'une autre personne : %d, 404 attendu", rec.Code)
	}
	// La route des images locales est montée (lecture seule).
	if rec := b.get("/images/" + strings.Repeat("0", 64) + ".png"); rec.Code != http.StatusNotFound {
		t.Errorf("image absente : %d, 404 attendu", rec.Code)
	}
}

// giverOf renvoie le tireur de la personne receiver (tirage actuel).
func (e *revealEnv) giverOf(receiver int64) int64 {
	e.t.Helper()
	var g int64
	err := e.a.DB.QueryRow(`SELECT p.giver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' AND p.receiver_id = ?`, e.gid, receiver).Scan(&g)
	if err != nil {
		e.t.Fatal(err)
	}
	return g
}

func (e *revealEnv) emailOf(id int64) string {
	e.t.Helper()
	var m string
	if err := e.a.DB.QueryRow(`SELECT email FROM participants WHERE id = ?`, id).Scan(&m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// Une lettre écrite prévient le tireur (un courriel regroupé), sans nommer la
// cible ; l'enregistrement de la lettre n'en dépend pas.
func TestWave11GiverNotifiedOfNewIdea(t *testing.T) {
	a := wave8App(t)
	if a.GiverNotify == nil {
		t.Fatal("service de notification du tireur non construit")
	}
	e := newRevealEnv(t, a)
	e.drawNow()
	ctx := context.Background()
	receiver := e.ids[0]
	giver := e.giverOf(receiver)
	if l, err := a.Letter.SetIntro(ctx, e.gid, receiver, "Un vélo rouge"); err != nil || l.Version < 1 {
		t.Fatalf("lettre non enregistrée : %+v %v", l, err)
	}
	to := e.emailOf(giver)
	if n := countWhere(t, a, `SELECT COUNT(*) FROM mail_queue WHERE recipient = ? AND mail_type = ?`, to, mailunsub.TypeNouvelleIdee); n != 1 {
		t.Fatalf("%d notification(s) « nouvelle idée » au tireur, 1 attendue", n)
	}
	// Une deuxième modification est regroupée, pas doublée.
	if _, err := a.Letter.SetIntro(ctx, e.gid, receiver, "Un vélo bleu"); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM mail_queue WHERE recipient = ? AND mail_type = ? AND status = 'pending'`, to, mailunsub.TypeNouvelleIdee); n != 1 {
		t.Errorf("%d notification(s) en attente, 1 attendue (regroupement)", n)
	}
	body := queuedMail(t, a, to, mailunsub.TypeNouvelleIdee)
	if first := strings.Fields(e.names[0])[0]; strings.Contains(body, first) {
		t.Errorf("le courriel du tireur nomme la cible (%s) :\n%s", first, body)
	}
	// La langue du message : Content-Language suit la langue du destinataire.
	if n := countWhere(t, a, `SELECT COUNT(*) FROM mail_queue WHERE recipient = ? AND lang <> ''`, to); n == 0 {
		t.Error("la langue du message n'est pas enregistrée en file")
	}
}

// La relance « lettre non lue » du tireur (givernotify.Tick) et la relance
// manuelle de lecture (reminders.Engine.Remind) partagent le courriel du jour :
// quel que soit l'ordre, le tireur ne reçoit pas deux relances le même jour.
func TestWave11GiverReminderNeverDoublesManualReminder(t *testing.T) {
	a := wave8App(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	ctx := context.Background()
	for _, r := range e.ids[:2] {
		if _, err := a.Letter.SetIntro(ctx, e.gid, r, "Mot"); err != nil {
			t.Fatal(err)
		}
	}
	a.Clock.(*clock.Fake).Advance(5 * 24 * time.Hour) // 10 h : dans la fenêtre d'envoi
	g1, g2 := e.giverOf(e.ids[0]), e.giverOf(e.ids[1])
	if g1 == g2 {
		t.Skip("même tireur pour les deux lettres : le jeu d'essai ne distingue pas les deux ordres")
	}
	relances := func(id int64) int {
		return countWhere(t, a, `SELECT COUNT(*) FROM mail_queue WHERE recipient = ? AND mail_type = ?`, e.emailOf(id), mailunsub.TypeRelances)
	}

	// Manuelle d'abord, puis automatique.
	res, err := a.Reminders.Remind(ctx, e.gid, reminders.KindRead, []int64{g1})
	if err != nil || res.Count(reminders.OutcomeQueued) != 1 {
		t.Fatalf("relance manuelle : %+v %v", res, err)
	}
	if _, err := a.GiverNotify.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if n := relances(g1); n != 1 {
		t.Errorf("manuelle puis automatique : %d relance(s) pour le tireur 1, 1 attendue", n)
	}
	// Automatique d'abord, puis manuelle.
	if n := relances(g2); n != 1 {
		t.Fatalf("relance automatique du tireur 2 : %d, 1 attendue", n)
	}
	res, err = a.Reminders.Remind(ctx, e.gid, reminders.KindRead, []int64{g2})
	if err != nil || res.Count(reminders.OutcomeAlreadyToday) != 1 {
		t.Fatalf("relance manuelle après l'automatique : %+v %v", res, err)
	}
	if n := relances(g2); n != 1 {
		t.Errorf("automatique puis manuelle : %d relance(s) pour le tireur 2, 1 attendue", n)
	}
	// Le lendemain, l'automatique n'a pas épuisé le plafond de la manuelle.
	if n := countWhere(t, a, `SELECT COUNT(*) FROM reminders_sent WHERE kind = 'lecture' AND participant_id = ? AND automatic = 0`, g1); n != 1 {
		t.Errorf("relance manuelle non journalisée : %d", n)
	}
}

// La langue du destinataire est enregistrée avec le courriel en file (puis sert
// à Content-Language) : « tirage prêt » part en anglais pour qui a choisi
// l'anglais, en français sinon.
func TestWave11MailLangFollowsRecipient(t *testing.T) {
	a := wave8App(t)
	e := newRevealEnv(t, a)
	ctx := context.Background()
	if err := a.Participants.SetLocale(ctx, participants.ParticipantActor(e.gid, e.ids[0]), e.gid, e.ids[0], "en"); err != nil {
		t.Fatal(err)
	}
	e.drawNow()
	for i, want := range map[int]string{0: "en", 1: "fr"} {
		var lang string
		err := a.DB.QueryRow(`SELECT lang FROM mail_queue WHERE recipient = ? AND mail_type LIKE 'ready%'`, e.emailOf(e.ids[i])).Scan(&lang)
		if err != nil {
			t.Fatalf("avis « tirage prêt » de la personne %d : %v", i, err)
		}
		if lang != want {
			t.Errorf("personne %d : langue %q, %q attendu", i, lang, want)
		}
	}
}
