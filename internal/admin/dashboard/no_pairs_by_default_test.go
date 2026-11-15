package dashboard

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

var pairWords = regexp.MustCompile(`(?i)pair|receiver|giver|tireur|tire\b|cible\b|recipient|offers`)

// walk vérifie qu'aucun type de sortie ne peut porter une paire : pas de champ
// au nom de paire, pas de table de correspondance, seulement des comptes, des
// booléens, des textes et des lignes par personne.
func walk(t *testing.T, ty reflect.Type, path string) {
	t.Helper()
	switch ty.Kind() {
	case reflect.Struct:
		if ty == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			// Un compte entier (« tireurs à un cadeau ») n'est pas une paire.
			if pairWords.MatchString(f.Name) && f.Type.Kind() != reflect.Int {
				t.Errorf("%s.%s : un champ de sortie ressemble à une paire", path, f.Name)
			}
			walk(t, f.Type, path+"."+f.Name)
		}
	case reflect.Slice:
		walk(t, ty.Elem(), path+"[]")
	case reflect.Ptr:
		walk(t, ty.Elem(), path)
	case reflect.Map, reflect.Interface, reflect.Func, reflect.Chan, reflect.Array:
		t.Errorf("%s est de type %s : une sortie ne porte ni table, ni interface", path, ty)
	}
}

func TestOutputTypesCarryNoPair(t *testing.T) {
	walk(t, reflect.TypeOf(View{}), "View")
	walk(t, reflect.TypeOf(CheckResult{}), "CheckResult")
	walk(t, reflect.TypeOf(pageData{}), "pageData")
	// Les lignes de la page sont des personnes, jamais des couples.
	walk(t, reflect.TypeOf(personRow{}), "personRow")
}

// rowOf découpe, dans la page, la ligne d'une personne.
func rowOf(t *testing.T, body string, id int64) string {
	t.Helper()
	start := strings.Index(body, `id="personne-`+strconv.FormatInt(id, 10)+`"`)
	if start < 0 {
		t.Fatalf("ligne de la personne %d absente", id)
	}
	end := strings.Index(body[start:], "</li>")
	if end < 0 {
		t.Fatal("ligne non terminée")
	}
	return body[start : start+end]
}

func TestNoResponseCarriesAPairByDefault(t *testing.T) {
	e := newEnv(t)
	gid := e.group("pas-de-paire")
	ids := e.addN(gid, "Hortensia", 3, participants.RoleNormal)
	e.addN(gid, "Gaspardin", 3, participants.RoleOffrePlus)
	e.add(gid, "Mamie", "", participants.RoleReceptionSeule)
	e.doDraw(gid)
	e.audit.Record(ctx, admin(gid), gid, "lien_remis", ids[0])
	ck := e.adminCookie(gid)

	pairs := e.pairs(gid)
	if len(pairs) != 7 {
		t.Fatalf("paires du tirage : %d", len(pairs))
	}
	// Prénom et nom de chaque personne, par identifiant.
	list, err := e.parts.List(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]participants.Participant{}
	for _, p := range list {
		byID[p.ID] = p
	}

	for _, lang := range []string{"fr", "en"} {
		dash := e.get("/pas-de-paire/admin", lang, ck)
		if dash.Code != 200 {
			t.Fatalf("tableau de bord : %d", dash.Code)
		}
		body := dash.Body.String()
		for _, pr := range pairs {
			row := rowOf(t, body, pr[0])
			target := byID[pr[1]]
			// La ligne d'un tireur ne nomme jamais sa cible.
			if strings.Contains(row, target.FirstName+" "+target.Name) || strings.Contains(row, target.Name) {
				t.Errorf("la ligne du tireur %d nomme sa cible %d", pr[0], pr[1])
			}
		}
		for _, page := range []string{"/pas-de-paire/admin/controle", "/pas-de-paire/admin/journal"} {
			rec := e.get(page, lang, ck)
			if rec.Code != 200 {
				t.Fatalf("%s : %d", page, rec.Code)
			}
			for _, p := range list {
				if page != "/pas-de-paire/admin/journal" && strings.Contains(rec.Body.String(), p.Name) {
					t.Errorf("%s nomme %s : le contrôle ne montre aucun nom", page, p.Name)
				}
			}
		}
	}
}

func TestServiceResultsCarryNoPair(t *testing.T) {
	e := newEnv(t)
	gid := e.group("sans-paire")
	e.addN(gid, "Quentrix", 4, participants.RoleNormal)
	e.doDraw(gid)
	v, err := e.svc.Dashboard(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.CheckDraw(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Green {
		t.Fatalf("contrôle : %+v", c.Rules)
	}
	if v.Targets != 4 || v.TargetsSeen != 0 {
		t.Fatalf("comptes des cibles : %d vues sur %d", v.TargetsSeen, v.Targets)
	}
}
