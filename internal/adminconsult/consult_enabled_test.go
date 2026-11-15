package adminconsult

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// others renvoie les noms des personnes qui ne sont ni giver ni ses cibles.
func (e *env) others(giver int64, targets []int64, all []int64) []string {
	keep := map[int64]bool{giver: true}
	for _, t := range targets {
		keep[t] = true
	}
	var out []string
	for _, id := range all {
		if !keep[id] {
			out = append(out, e.names[id]...)
		}
	}
	return out
}

// F10b.AC2 : la consultation exige la ressaisie du mot de passe
// d'administration ; une session d'administration ne suffit pas.
func TestConsultationRequiresAdminPassword(t *testing.T) {
	e := newEnv(t)
	gid := e.group("ouvert", true)
	ids := e.addN(gid, "Hortense", 5)
	other := e.group("voisin", true)
	e.addN(other, "Ines", 3)
	e.doDraw(gid)
	ck := e.adminCookie(gid)
	giver := ids[1]
	targets := e.receiversOf(gid, giver)
	if len(targets) == 0 {
		t.Fatal("fixture : la personne doit avoir une cible")
	}

	cases := []struct {
		name     string
		password []string
		status   int
	}{
		{"sans mot de passe", nil, http.StatusUnprocessableEntity},
		{"mot de passe vide", []string{""}, http.StatusUnprocessableEntity},
		{"mot de passe faux", []string{"pas-le-bon-secret"}, http.StatusForbidden},
		{"secret d'un autre groupe", []string{e.secrets[other]}, http.StatusForbidden},
	}
	for _, c := range cases {
		f := url.Values{"person": {idStr(giver)}}
		if c.password != nil {
			f["password"] = c.password
		}
		rec := e.consult("ouvert", "fr", ck, f)
		if rec.Code != c.status {
			t.Errorf("%s : %d, attendu %d", c.name, rec.Code, c.status)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `aria-invalid="true" aria-describedby="ac-password-error"`) || !strings.Contains(body, `id="ac-password-error"`) {
			t.Errorf("%s : l'erreur doit s'afficher sous le champ du mot de passe", c.name)
		}
		if strings.Contains(body, "Paire consultée") {
			t.Errorf("%s : un résultat est affiché sans mot de passe valable", c.name)
		}
	}
	if n := e.journalRows(gid); n != 0 {
		t.Fatalf("journal : %d ligne(s) sans mot de passe valable", n)
	}

	// Service : même refus, avec un code reconnaissable.
	_, err := e.svc.Consult(ctx, participants.AdminActor(gid), gid, giver, "faux")
	if code, _ := errs.CodeOf(err); code != CodeBadPassword {
		t.Fatalf("service, mot de passe faux : %v", err)
	}
	// Un participant, même avec le bon mot de passe, n'est pas administrateur.
	if _, err := e.svc.Consult(ctx, participants.ParticipantActor(gid, ids[0]), gid, giver, e.secrets[gid]); err == nil {
		t.Fatal("service : un participant a pu consulter une paire")
	}
	rec := e.consult("ouvert", "fr", e.participantCookie(gid, ids[0]), url.Values{"person": {idStr(giver)}, "password": {e.secrets[gid]}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("page : un participant obtient %d", rec.Code)
	}
	if rec := e.get("/ouvert/admin/consultation", "fr", nil); rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != "/ouvert/admin/connexion" {
		t.Fatalf("visiteur sans session : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := e.journalRows(gid); n != 0 {
		t.Fatalf("journal : %d ligne(s) après des refus", n)
	}

	// Bon mot de passe : la paire de cette personne, et d'elle seule.
	rec = e.consult("ouvert", "fr", ck, url.Values{"person": {idStr(giver)}, "password": {e.secrets[gid]}})
	if rec.Code != http.StatusOK {
		t.Fatalf("bon mot de passe : %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, n := range e.names[giver] {
		if !strings.Contains(body, n) {
			t.Errorf("résultat : la personne consultée %q n'apparaît pas", n)
		}
	}
	for _, tg := range targets {
		for _, n := range e.names[tg] {
			if !strings.Contains(body, n) {
				t.Errorf("résultat : la cible %q n'apparaît pas", n)
			}
		}
	}
	for _, n := range e.others(giver, targets, ids) {
		if strings.Contains(body, n) {
			t.Errorf("résultat : %q apparaît alors que seule une personne est consultée", n)
		}
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("résultat : Cache-Control %q", cc)
	}
	if n := e.journalRows(gid); n != 1 {
		t.Fatalf("journal : %d ligne(s) après une consultation", n)
	}
}

// F10b.AC2 : une personne à la fois ; plusieurs personnes dans la même
// demande sont refusées sans rien révéler ni consigner.
func TestConsultationCoversOnePersonAtATime(t *testing.T) {
	e := newEnv(t)
	gid := e.group("une-seule", true)
	ids := e.addN(gid, "Hortense", 4)
	e.doDraw(gid)
	ck := e.adminCookie(gid)
	rec := e.consult("une-seule", "fr", ck, url.Values{"person": {idStr(ids[0]), idStr(ids[1])}, "password": {e.secrets[gid]}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("deux personnes : %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="ac-person-error"`) {
		t.Error("deux personnes : l'erreur doit s'afficher sous le choix de la personne")
	}
	for _, f := range []url.Values{
		{"password": {e.secrets[gid]}},
		{"person": {"tous"}, "password": {e.secrets[gid]}},
		{"person": {"0"}, "password": {e.secrets[gid]}},
		{"person": {"999999"}, "password": {e.secrets[gid]}},
	} {
		if rec := e.consult("une-seule", "fr", ck, f); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v : %d", f["person"], rec.Code)
		}
	}
	// Une personne d'un autre groupe n'est pas consultable.
	far := e.group("lointain", true)
	farIDs := e.addN(far, "Ines", 3)
	e.doDraw(far)
	if rec := e.consult("une-seule", "fr", ck, url.Values{"person": {idStr(farIDs[0])}, "password": {e.secrets[gid]}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("personne d'un autre groupe : %d", rec.Code)
	}
	if n := e.journalRows(gid) + e.journalRows(far); n != 0 {
		t.Fatalf("journal : %d ligne(s) pour des demandes refusées", n)
	}
	res, err := e.svc.Consult(ctx, participants.AdminActor(gid), gid, ids[2], e.secrets[gid])
	if err != nil {
		t.Fatal(err)
	}
	if res.Giver.ID != ids[2] {
		t.Fatalf("personne consultée %d, attendu %d", res.Giver.ID, ids[2])
	}
	want := e.receiversOf(gid, ids[2])
	if len(res.Targets) != len(want) {
		t.Fatalf("cibles : %d, attendu %d", len(res.Targets), len(want))
	}
	for i, tg := range res.Targets {
		if tg.ID != want[i] {
			t.Fatalf("cible %d : %d, attendu %d", i, tg.ID, want[i])
		}
	}
}

var csrfValueRe = regexp.MustCompile(`value="[A-Za-z0-9_-]{20,}"`)

// F10b.AC2 : aucun tableau complet. Le formulaire ne porte aucune paire (il
// est identique avant et après le tirage) et aucune méthode du service ne
// renvoie plus d'une personne consultée.
func TestNoFullTableIsOffered(t *testing.T) {
	e := newEnv(t)
	gid := e.group("sans-tableau", true)
	e.addN(gid, "Hortense", 5)
	ck := e.adminCookie(gid)
	before := e.get("/sans-tableau/admin/consultation", "fr", ck)
	if before.Code != http.StatusOK {
		t.Fatalf("formulaire : %d", before.Code)
	}
	e.doDraw(gid)
	after := e.get("/sans-tableau/admin/consultation", "fr", ck)
	strip := func(s string) string { return csrfValueRe.ReplaceAllString(s, `value=""`) }
	if strip(before.Body.String()) != strip(after.Body.String()) {
		t.Fatal("le formulaire change après le tirage : il porte des informations de paire")
	}
	if strings.Contains(after.Body.String(), "<table") {
		t.Fatal("le formulaire contient un tableau")
	}

	consultation := reflect.TypeOf(Consultation{})
	st := reflect.TypeOf(&Service{})
	for i := 0; i < st.NumMethod(); i++ {
		m := st.Method(i)
		for o := 0; o < m.Type.NumOut(); o++ {
			out := m.Type.Out(o)
			for out.Kind() == reflect.Pointer || out.Kind() == reflect.Slice || out.Kind() == reflect.Map {
				if out.Kind() != reflect.Pointer && out.Elem() == consultation ||
					out.Kind() != reflect.Pointer && out.Elem().Kind() == reflect.Pointer && out.Elem().Elem() == consultation {
					t.Errorf("%s renvoie plusieurs consultations", m.Name)
				}
				out = out.Elem()
			}
		}
		if m.Name == "Consult" {
			// (récepteur, ctx, acteur, groupe, UNE personne, mot de passe)
			if m.Type.NumIn() != 6 || m.Type.In(4).Kind() != reflect.Int64 {
				t.Errorf("Consult doit porter sur une seule personne : %s", m.Type)
			}
		}
	}
	// Person ne porte jamais de cible : la liste de choix ne dit rien des paires.
	for i := 0; i < reflect.TypeOf(Person{}).NumField(); i++ {
		if f := reflect.TypeOf(Person{}).Field(i); f.Type.Kind() == reflect.Slice || f.Type.Kind() == reflect.Pointer {
			t.Errorf("Person.%s pourrait porter une paire", f.Name)
		}
	}
}

// F10b.AC2 : avant le tirage, il n'y a rien à consulter ; les échecs répétés
// du mot de passe sont bornés.
func TestNoDrawAndPasswordAttemptsAreLimited(t *testing.T) {
	e := newEnv(t)
	gid := e.group("avant", true)
	ids := e.addN(gid, "Hortense", 4)
	ck := e.adminCookie(gid)
	if rec := e.consult("avant", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}}); rec.Code != http.StatusConflict {
		t.Fatalf("avant le tirage : %d", rec.Code)
	}
	if n := e.journalRows(gid); n != 0 {
		t.Fatalf("journal avant le tirage : %d", n)
	}
	e.doDraw(gid)
	for i := 0; i < PasswordAttempts.Limit; i++ {
		if rec := e.consult("avant", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {"faux"}}); rec.Code != http.StatusForbidden {
			t.Fatalf("échec %d : %d", i+1, rec.Code)
		}
	}
	rec := e.consult("avant", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("après %d échecs, même le bon mot de passe doit attendre : %d", PasswordAttempts.Limit, rec.Code)
	}
	if _, err := e.svc.Consult(ctx, participants.AdminActor(gid), gid, ids[0], e.secrets[gid]); !errors.Is(err, limiter.ErrLimited) {
		t.Fatalf("service : %v", err)
	}
	e.clock.Advance(PasswordAttempts.Window)
	if rec := e.consult("avant", "fr", ck, url.Values{"person": {idStr(ids[0])}, "password": {e.secrets[gid]}}); rec.Code != http.StatusOK {
		t.Fatalf("après la fenêtre : %d", rec.Code)
	}
}
