package dashboard

import (
	"strings"
	"testing"

	"secretsanta/internal/participants"
)

// F10.AC28 : décomptes sans nom, et chacun reçoit exactement un cadeau.
func TestCheckCountsAreNamelessAndEveryoneReceivesOneGift(t *testing.T) {
	e := newEnv(t)
	gid := e.group("decomptes")
	e.addN(gid, "Hortensia", 2, participants.RoleReceptionSeule)
	e.addN(gid, "Gaspardin", 1, participants.RoleOffrePlus)
	e.addN(gid, "Xavarin", 3, participants.RoleNormal)
	e.addN(gid, "Quentrix", 2, participants.RoleUnSeul)
	e.doDraw(gid)

	res, err := e.svc.CheckDraw(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Green || !ruleOK(*res, RuleEachReceivesOne) {
		t.Fatalf("rapport : %+v", res.Rules)
	}
	// 8 personnes reçoivent un cadeau ; 6 tireurs offrent 8 cadeaux au total.
	if res.Beneficiaries != 8 {
		t.Fatalf("bénéficiaires : %d", res.Beneficiaries)
	}
	if res.GiversOneGift+res.GiversTwoGifts+res.GiversMoreGifts != 6 {
		t.Fatalf("tireurs : %d + %d + %d", res.GiversOneGift, res.GiversTwoGifts, res.GiversMoreGifts)
	}
	if res.GiversOneGift+2*res.GiversTwoGifts+3*res.GiversMoreGifts != 8 {
		t.Fatalf("cadeaux offerts : %+v", res)
	}
	if res.Roles != (RoleCounts{Normal: 3, OffrePlus: 1, UnSeul: 2, ReceptionSeule: 2}) {
		t.Fatalf("rôles : %+v", res.Roles)
	}
	if res.Cap != 2 {
		t.Fatalf("plafond : %d", res.Cap)
	}
	// Les un_seul n'offrent qu'un cadeau et la réception seule aucun : les
	// seuls tireurs à deux cadeaux sont donc des normaux ou des offre_plus.
	if res.GiversTwoGifts != 2 {
		t.Fatalf("tireurs à deux cadeaux : %d", res.GiversTwoGifts)
	}

	// Les mêmes décomptes s'affichent, sans aucun nom.
	for _, lang := range []string{"fr", "en"} {
		rec := e.get("/decomptes/admin/controle", lang, e.adminCookie(gid))
		if rec.Code != 200 {
			t.Fatalf("page : %d", rec.Code)
		}
		body := rec.Body.String()
		for _, n := range e.names {
			if strings.Contains(body, n) {
				t.Errorf("la page de contrôle nomme %q", n)
			}
		}
		if !strings.Contains(body, "<dd>8</dd>") || !strings.Contains(body, "<dd>2</dd>") || !strings.Contains(body, "<dd>3</dd>") {
			t.Errorf("les décomptes ne sont pas affichés : %s", body)
		}
	}
}
