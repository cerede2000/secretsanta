package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/groupsettings"
	"secretsanta/internal/participants"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/shared/clock"
)

// Les quatre modules de la vague 10 sont appliqués et leurs services bâtis.
func TestWave10ModulesAndServicesBuilt(t *testing.T) {
	a := wave8App(t)
	for _, table := range []string{"reminders_sent", "reminder_link_opens", "adminconsult_journal"} {
		var n int
		if err := a.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Errorf("table %s absente : %v", table, err)
		}
	}
	if a.Reminders == nil || a.Suggestions == nil || a.Repair == nil || a.RuleChange == nil {
		t.Fatalf("service manquant : relances %v, suggestions %v, réparation %v, règles %v",
			a.Reminders != nil, a.Suggestions != nil, a.Repair != nil, a.RuleChange != nil)
	}
	// Sans transport, le moteur de relances n'est pas bâti mais le fait « lien
	// ouvert » reste enregistrable.
	b := newApp(t)
	if b.Reminders != nil {
		t.Error("le moteur de relances ne doit pas exister sans file d'envoi")
	}
	if b.ReminderFacts.DB == nil {
		t.Error("ReminderFacts doit toujours être construit")
	}
}

// La première connexion par lien personnel est notée ; le planificateur ne
// relance plus pour le lien la personne qui l'a ouvert, et relance l'autre.
func TestWave10LinkOpenedStopsLinkReminder(t *testing.T) {
	e := newW9(t)
	ctx := context.Background()
	var ids []int64
	for _, in := range []struct{ first, mail string }{{"Léa", "lea@example.org"}, {"Hugo", "hugo@example.org"}} {
		p, err := e.a.Participants.Create(ctx, e.actor, e.gid, participantsInput(in.first, in.mail))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	tok, err := e.a.Tokens.IssueLink(ctx, itoa(e.gid), itoa(ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, e.a)
	link := "/noel/acces/" + tok
	if rec := b.get(link); rec.Code != http.StatusOK {
		t.Fatalf("aperçu du lien : %d", rec.Code)
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM reminder_link_opens`); n != 0 {
		t.Fatalf("le simple affichage a noté une ouverture (%d)", n)
	}
	if rec := b.post(link, link, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("connexion par lien : %d", rec.Code)
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM reminder_link_opens WHERE group_id = ? AND participant_id = ?`, e.gid, ids[0]); n != 1 {
		t.Fatalf("ouverture non notée (%d)", n)
	}
	// Rejouer le lien ne double rien.
	b2 := newBrowser(t, e.a)
	b2.post(link, link, url.Values{})
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM reminder_link_opens`); n != 1 {
		t.Fatalf("%d ouvertures notées, attendu 1", n)
	}

	e.a.Clock.(*clock.Fake).Advance(4 * 24 * time.Hour)
	res, err := e.a.Reminders.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Queued != 2 {
		t.Fatalf("%d relances mises en file, attendu 2 (lettre pour l'une, lien pour l'autre)", res.Queued)
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM reminders_sent WHERE participant_id = ? AND kind = 'lien'`, ids[0]); n != 0 {
		t.Errorf("relance de lien envoyée à la personne qui l'a ouvert")
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM reminders_sent WHERE participant_id = ? AND kind = 'lien'`, ids[1]); n != 1 {
		t.Errorf("relance de lien absente pour la personne qui ne l'a pas ouvert")
	}
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM mail_queue WHERE mail_type = 'relances'`); n != 2 {
		t.Errorf("%d courriels de relance en file, attendu 2", n)
	}
}

// Les pages d'exploitation et de consultation sont montées ; la consultation
// ne l'est que si l'option du groupe est active, et la feuille « Plus » la
// propose dans ce seul cas.
func TestWave10AdminOpsAndConsultRoutes(t *testing.T) {
	e := newW9(t)
	anon := newBrowser(t, e.a)
	for _, p := range []string{"/noel/admin/donnees", "/noel/admin/participants/export"} {
		if rec := anon.get(p); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/noel/admin/connexion" {
			t.Errorf("%s sans session : %d %q", p, rec.Code, rec.Header().Get("Location"))
		}
		if rec := e.admin.get(p); rec.Code != http.StatusOK {
			t.Errorf("%s : %d", p, rec.Code)
		}
	}
	if rec := e.admin.get("/noel/admin/participants/export"); !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Errorf("export : type %q", rec.Header().Get("Content-Type"))
	}
	if rec := e.admin.get("/noel/admin/consultation"); rec.Code != http.StatusNotFound {
		t.Errorf("consultation avec l'option désactivée : %d, attendu 404", rec.Code)
	}
	if plus := e.admin.get("/noel/admin/plus").Body.String(); strings.Contains(plus, "/noel/admin/consultation") {
		t.Error("la feuille Plus propose la consultation alors que l'option est désactivée")
	}

	if _, err := e.a.DB.Exec(`UPDATE groups SET admin_can_view_pair = 1 WHERE id = ?`, e.gid); err != nil {
		t.Fatal(err)
	}
	if rec := e.admin.get("/noel/admin/consultation"); rec.Code != http.StatusOK {
		t.Errorf("consultation avec l'option active : %d", rec.Code)
	}
	if rec := e.admin.get("/noel/consultations"); rec.Code != http.StatusOK {
		t.Errorf("journal des consultations : %d", rec.Code)
	}
	plus := e.admin.get("/noel/admin/plus").Body.String()
	if !strings.Contains(plus, `href="/noel/admin/consultation"`) {
		t.Errorf("la feuille Plus ne propose pas la consultation :\n%s", plus)
	}
	if strings.Contains(plus, "admin.menu.consult") {
		t.Error("clé de texte brute dans la feuille Plus")
	}
}

// L'interrupteur « choix du rôle » de l'administration est monté.
func TestWave10RoleChoiceSwitchRoute(t *testing.T) {
	e := newW9(t)
	e.enableLink()
	rec := e.admin.post("/noel/admin/inscriptions", "/noel/admin/inscriptions/choix-role", url.Values{"enabled": {"1"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "role_on") {
		t.Fatalf("interrupteur : %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if n := countWhere(t, e.a, `SELECT role_choice FROM registration_invitations WHERE group_id = ?`, e.gid); n != 1 {
		t.Errorf("role_choice = %d, attendu 1", n)
	}
	rec = e.admin.post("/noel/admin/inscriptions", "/noel/admin/inscriptions/choix-role", url.Values{"enabled": {"0"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("désactivation : %d", rec.Code)
	}
	if n := countWhere(t, e.a, `SELECT role_choice FROM registration_invitations WHERE group_id = ?`, e.gid); n != 0 {
		t.Errorf("role_choice = %d, attendu 0", n)
	}
}

// La réparation réelle remplace l'adaptateur temporaire : une exclusion qui
// contredit une paire du tirage donne un aperçu, puis une réparation qui
// change la paire.
func TestWave10RealRepairBehindRuleChange(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	ctx := context.Background()
	var giver, receiver int64
	if err := a.DB.QueryRow(`SELECT giver_id, receiver_id FROM draw_pairs WHERE draw_id = (SELECT id FROM draws WHERE group_id = ? AND status = 'actuel') ORDER BY id LIMIT 1`, e.gid).
		Scan(&giver, &receiver); err != nil {
		t.Fatal(err)
	}
	out, err := a.RuleChange.AddExclusion(ctx, e.gid, giver, receiver, rulechange.ChoiceAsk)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != rulechange.StatusNeedsConfirmation || out.Preview.AffectedCount == 0 || !out.Preview.Repairable {
		t.Fatalf("aperçu attendu avec des personnes touchées : %+v", out)
	}
	out, err = a.RuleChange.AddExclusion(ctx, e.gid, giver, receiver, rulechange.ChoiceRepair)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Repaired || out.Status != rulechange.StatusSaved {
		t.Fatalf("réparation non appliquée : %+v", out)
	}
	if n := countWhere(t, a, `SELECT COUNT(*) FROM draw_pairs WHERE draw_id = (SELECT id FROM draws WHERE group_id = ? AND status = 'actuel') AND giver_id = ? AND receiver_id = ?`, e.gid, giver, receiver); n != 0 {
		t.Errorf("la paire exclue existe encore")
	}
}

// « Prévenir les participants » à un changement de budget met le courriel
// BudgetUpdated en file, dans la langue du destinataire ; sans la case, rien.
func TestWave10BudgetChangeNotifiesWhenAsked(t *testing.T) {
	e := newW9(t)
	ctx := context.Background()
	for _, in := range []struct{ first, mail string }{{"Léa", "lea@example.org"}, {"Hugo", "hugo@example.org"}} {
		if _, err := e.a.Participants.Create(ctx, e.actor, e.gid, participantsInput(in.first, in.mail)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.a.Participants.Create(ctx, e.actor, e.gid, participantsInput("Zoé", "")); err != nil {
		t.Fatal(err)
	}
	update := func(budget string, notify bool) {
		t.Helper()
		if _, err := e.a.GroupSettings.UpdateGroupSettings(ctx, groupSettingsInput(e.gid, "Famille Noël", budget, notify)); err != nil {
			t.Fatal(err)
		}
	}
	update("25", false)
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM mail_queue WHERE mail_type = 'budget_updated'`); n != 0 {
		t.Fatalf("%d courriels sans la case cochée", n)
	}
	update("30", true)
	if n := countWhere(t, e.a, `SELECT COUNT(*) FROM mail_queue WHERE mail_type = 'budget_updated' AND essential = 1`); n != 2 {
		t.Fatalf("%d courriels de budget, attendu 2 (la personne sans adresse est ignorée)", n)
	}
	if body := queuedMail(t, e.a, "lea@example.org", "budget_updated"); !strings.Contains(body, "30") {
		t.Errorf("le courriel ne donne pas le nouveau budget :\n%s", body)
	}
}

// La révélation inclut le style « carte à gratter » : ses ressources sont dans
// la page.
func TestWave10RevealPageCarriesScratchStyle(t *testing.T) {
	a := newApp(t)
	e := newRevealEnv(t, a)
	e.drawNow()
	page := e.login(0).get("/" + revealSlug + "/decouverte")
	if page.Code != http.StatusOK {
		t.Fatalf("page : %d", page.Code)
	}
	for _, want := range []string{"data-reveal-gift", "data-envelope-labels", "data-reveal-scratch", "/static/js/reveal/scratch/scratch.js", "/static/css/reveal/scratch/scratch.css"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("la page de révélation ne contient pas %q", want)
		}
	}
}

func participantsInput(first, email string) participants.Input {
	return participants.Input{FirstName: first, Name: "Test", Email: email}
}

func groupSettingsInput(gid int64, name, budget string, notify bool) groupsettings.Input {
	return groupsettings.Input{GroupID: gid, Name: name, Budget: budget, NotifyBudget: notify}
}
