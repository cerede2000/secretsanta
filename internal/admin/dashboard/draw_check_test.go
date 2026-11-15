package dashboard

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
)

func ruleOK(res CheckResult, r Rule) bool {
	for _, x := range res.Rules {
		if x.Rule == r {
			return x.OK
		}
	}
	return false
}

// base : trois tireurs normaux, tirés en cercle, sans courriel.
func base() checkInput {
	return checkInput{
		cap: 2,
		people: []checkPerson{
			{id: 1, role: participants.RoleNormal}, {id: 2, role: participants.RoleNormal}, {id: 3, role: participants.RoleNormal},
		},
		pairs: []drawPair{{1, 2}, {2, 3}, {3, 1}},
	}
}

func TestEvaluateGreenForAValidDraw(t *testing.T) {
	res := evaluate(base())
	if !res.Green {
		t.Fatalf("rapport rouge : %+v", res.Rules)
	}
	if len(res.Rules) != 8 {
		t.Fatalf("règles : %d", len(res.Rules))
	}
}

// F10.AC3 : rouge pour chaque règle violée.
func TestEvaluateIsRedForEachViolatedRule(t *testing.T) {
	cases := []struct {
		name   string
		rule   Rule
		mutate func(in *checkInput)
	}{
		{"deux cadeaux reçus", RuleEachReceivesOne, func(in *checkInput) { in.pairs = []drawPair{{1, 2}, {3, 2}, {2, 1}} }},
		{"personne ne reçoit", RuleEachReceivesOne, func(in *checkInput) { in.pairs = in.pairs[:2] }},
		{"un tireur n'offre rien", RuleGiversAtLeastOne, func(in *checkInput) { in.pairs = []drawPair{{1, 2}, {1, 3}, {2, 1}} }},
		{"plafond dépassé", RuleWithinCap, func(in *checkInput) {
			in.people = append(in.people, checkPerson{id: 4, role: participants.RoleNormal})
			in.pairs = []drawPair{{1, 2}, {1, 3}, {1, 4}, {2, 1}}
		}},
		{"un_seul offre deux cadeaux", RuleOneGiftOnly, func(in *checkInput) {
			in.people[0].role = participants.RoleUnSeul
			in.pairs = []drawPair{{1, 2}, {1, 3}, {2, 1}, {3, 1}}
		}},
		{"tirage de soi-même", RuleNoSelf, func(in *checkInput) { in.pairs = []drawPair{{1, 1}, {2, 3}, {3, 2}} }},
		{"exclusion violée", RuleExclusions, func(in *checkInput) { in.exclusions = []rules.Pair{{Giver: 1, Receiver: 2}} }},
		{"courriel invalide", RuleEmailsValid, func(in *checkInput) { in.people[0].email = "pas une adresse" }},
		{"lien pas envoyé", RuleLinksSent, func(in *checkInput) { in.people[1].email = "b@exemple.fr" }},
		{"réception seule qui offre", RuleWithinCap, func(in *checkInput) { in.people[0].role = participants.RoleReceptionSeule }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base()
			c.mutate(&in)
			res := evaluate(in)
			if res.Green {
				t.Fatal("le rapport devrait être rouge")
			}
			if ruleOK(res, c.rule) {
				t.Fatalf("la règle %s devrait être rouge : %+v", c.rule, res.Rules)
			}
		})
	}
}

func TestEvaluateAcceptsSentLinksAndPeopleWithoutEmail(t *testing.T) {
	in := base()
	in.people[0].email, in.people[0].everSent = "a@exemple.fr", true
	in.people[1].email, in.people[1].everSent = "Camille <b@exemple.fr>", true
	res := evaluate(in)
	if ruleOK(res, RuleEmailsValid) {
		t.Fatal("une adresse avec nom affiché n'est pas une adresse simple valide")
	}
	in.people[1].email = "b@exemple.fr"
	if res := evaluate(in); !res.Green {
		t.Fatalf("adresses valides et liens envoyés : %+v", res.Rules)
	}
}

// Sur une vraie base : un tirage valide est vert ; chaque corruption rend rouge
// la règle correspondante.
func TestCheckDrawOnRealDraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group("controle")
	ids := e.addN(gid, "Hortensia", 5, participants.RoleNormal)
	e.doDraw(gid)
	res, err := e.svc.CheckDraw(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Green || res.Beneficiaries != 5 {
		t.Fatalf("tirage valide : %+v", res)
	}

	// Une exclusion posée après le tirage contredit une paire existante.
	p := e.pairs(gid)[0]
	if _, err := e.db.Exec(`INSERT INTO manual_exclusions (group_id, giver_id, receiver_id) VALUES (?, ?, ?)`, gid, p[0], p[1]); err != nil {
		t.Fatal(err)
	}
	res, _ = e.svc.CheckDraw(ctx, admin(gid), gid)
	if res.Green || ruleOK(*res, RuleExclusions) {
		t.Fatalf("exclusion violée : %+v", res.Rules)
	}
	if _, err := e.db.Exec(`DELETE FROM manual_exclusions`); err != nil {
		t.Fatal(err)
	}

	// Un courriel invalide, puis un lien jamais envoyé.
	if _, err := e.db.Exec(`UPDATE participants SET email = 'oups' WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	res, _ = e.svc.CheckDraw(ctx, admin(gid), gid)
	if res.Green || ruleOK(*res, RuleEmailsValid) {
		t.Fatalf("courriel invalide : %+v", res.Rules)
	}
	if _, err := e.db.Exec(`UPDATE participants SET email = 'a@exemple.fr' WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	res, _ = e.svc.CheckDraw(ctx, admin(gid), gid)
	if res.Green || ruleOK(*res, RuleLinksSent) {
		t.Fatalf("lien non envoyé : %+v", res.Rules)
	}
	e.mail(gid, ids[0], "sent")
	if res, _ = e.svc.CheckDraw(ctx, admin(gid), gid); !res.Green {
		t.Fatalf("après envoi : %+v", res.Rules)
	}

	// Une affectation disparue : quelqu'un ne reçoit plus rien.
	if _, err := e.db.Exec(`DELETE FROM draw_pairs WHERE id = (SELECT MIN(id) FROM draw_pairs)`); err != nil {
		t.Fatal(err)
	}
	res, _ = e.svc.CheckDraw(ctx, admin(gid), gid)
	if res.Green || ruleOK(*res, RuleEachReceivesOne) {
		t.Fatalf("affectation manquante : %+v", res.Rules)
	}

	// La page du rapport rouge ne contient aucun nom.
	body := e.get("/controle/admin/controle", "fr", e.adminCookie(gid)).Body.String()
	if !strings.Contains(body, "role=\"alert\"") {
		t.Error("le rapport rouge doit être annoncé")
	}
	for _, n := range e.names {
		if strings.Contains(body, n) {
			t.Errorf("le rapport nomme %q", n)
		}
	}
}
