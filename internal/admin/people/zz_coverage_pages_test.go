package people

import (
	"net/http"
	"net/url"
	"testing"

	"secretsanta/internal/participants"
)

type zzCall struct {
	name   string
	method string
	path   string
	form   url.Values
	want   func(code int) bool
}

func zzRedirect(c int) bool { return c == http.StatusSeeOther }
func zzError(c int) bool    { return c >= 400 }
func zzOK(c int) bool       { return c == http.StatusOK }
func zz404(c int) bool      { return c == http.StatusNotFound }

func TestZZPeopleHandlers(t *testing.T) {
	e := newEnv(t)
	gid := e.groupA
	a := e.person(t, gid, "Alice", "alice@example.org", participants.RoleNormal)
	b := e.person(t, gid, "Bruno", "bruno@example.org", participants.RoleNormal)
	c := e.person(t, gid, "Carla", "", participants.RoleNormal)
	d := e.person(t, gid, "Denis", "denis@example.org", participants.RoleNormal)
	cookies := e.adminCookies(t, gid)
	base := "/noel/admin/participants"
	A, B, C, D := sid(a), sid(b), sid(c), sid(d)

	calls := []zzCall{
		{"fiche", "GET", base + "/" + A + "?regenerer=1&ok=resent", nil, zzOK},
		{"fiche-id-invalide", "GET", base + "/abc", nil, zz404},
		{"fiche-inconnue", "GET", base + "/9999", nil, zz404},
		{"modifier-form", "GET", base + "/" + A + "/modifier", nil, zzOK},
		{"modifier-form-id", "GET", base + "/abc/modifier", nil, zz404},
		{"modifier-form-inconnue", "GET", base + "/9999/modifier", nil, zz404},
		{"modifier-ok", "POST", base + "/" + A + "/modifier", url.Values{"first_name": {"Alice"}, "name": {"Nouveau"}, "email": {"alice@example.org"}}, zzRedirect},
		{"modifier-conflit", "POST", base + "/" + A + "/modifier", url.Values{"first_name": {"Bruno"}, "name": {"ZxBrunoovitch"}}, zzError},
		{"modifier-invalide", "POST", base + "/" + A + "/modifier", url.Values{"first_name": {""}, "name": {"X"}}, zzError},
		{"modifier-id", "POST", base + "/abc/modifier", nil, zz404},
		{"modifier-inconnue", "POST", base + "/9999/modifier", nil, zz404},
		{"ajout-invalide", "POST", base, url.Values{"first_name": {"Zoe"}, "name": {""}}, zzError},
		{"ajout-courriel", "POST", base, url.Values{"first_name": {"Zoe"}, "name": {"Zed"}, "email": {"pas-un-courriel"}}, zzError},
		{"ajout-conflit", "POST", base, url.Values{"first_name": {"Carla"}, "name": {"ZxCarlaovitch"}}, zzError},
		{"ajout", "POST", base, url.Values{"first_name": {"Zoe"}, "name": {"Zed"}}, zzRedirect},
		{"renvoyer-sans-courriel", "POST", base + "/" + C + "/renvoyer", nil, zzError},
		{"renvoyer-id", "POST", base + "/abc/renvoyer", nil, zz404},
		{"renvoyer-inconnue", "POST", base + "/9999/renvoyer", nil, zz404},
		{"renvoyer", "POST", base + "/" + B + "/renvoyer", nil, zzRedirect},
		{"adresse-vide", "POST", base + "/" + B + "/adresse", url.Values{"email": {""}}, zzError},
		{"adresse-invalide", "POST", base + "/" + B + "/adresse", url.Values{"email": {"nimporte"}}, zzError},
		{"adresse-id", "POST", base + "/abc/adresse", nil, zz404},
		{"adresse-inconnue", "POST", base + "/9999/adresse", nil, zz404},
		{"adresse", "POST", base + "/" + C + "/adresse", url.Values{"email": {"carla@example.org"}}, zzRedirect},
		{"regenerer-id", "POST", base + "/abc/regenerer", nil, zz404},
		{"regenerer-inconnue", "POST", base + "/9999/regenerer", nil, zz404},
		{"regenerer", "POST", base + "/" + B + "/regenerer", nil, zzOK},
		{"qr-id", "GET", base + "/abc/qr/x", nil, zz404},
		{"qr-inconnu", "GET", base + "/" + B + "/qr/inconnu", nil, zz404},
		// Règles.
		{"regle-type", "POST", base + "/regles", url.Values{"kind": {"inconnu"}}, zzError},
		{"regle-exclusion-id", "POST", base + "/regles", url.Values{"kind": {kindExclusion}, "a": {"x"}, "b": {B}}, zzError},
		{"regle-exclusion-id2", "POST", base + "/regles", url.Values{"kind": {kindExclusion}, "a": {A}, "b": {"x"}}, zzError},
		{"regle-exclusion", "POST", base + "/regles", url.Values{"kind": {kindExclusion}, "a": {A}, "b": {B}, "both": {"1"}}, zzRedirect},
		{"regle-exclusion-soi", "POST", base + "/regles", url.Values{"kind": {kindExclusion}, "a": {A}, "b": {A}}, zzError},
		{"regle-forcee-id", "POST", base + "/regles", url.Values{"kind": {kindForced}, "a": {"x"}}, zzError},
		{"regle-forcee", "POST", base + "/regles", url.Values{"kind": {kindForced}, "a": {C}, "b": {D}}, zzRedirect},
		{"regle-foyer-membres", "POST", base + "/regles", url.Values{"kind": {kindHousehold}, "members": {"x"}, "name": {"Foyer"}}, zzError},
		{"regle-foyer", "POST", base + "/regles", url.Values{"kind": {kindHousehold}, "members": {A, B}, "name": {"Foyer"}, "choice": {"future"}}, zzRedirect},
		{"regle-membre-id", "POST", base + "/regles", url.Values{"kind": {kindMember}, "household": {"x"}}, zzError},
		{"regle-membre-id2", "POST", base + "/regles", url.Values{"kind": {kindMember}, "household": {"1"}, "a": {"x"}}, zzError},
		{"regle-membre", "POST", base + "/regles", url.Values{"kind": {kindMember}, "household": {"1"}, "a": {D}, "choice": {"repair"}}, zzRedirect},
		{"retirer-type", "POST", base + "/regles/retirer", url.Values{"kind": {"inconnu"}}, zzError},
		{"retirer-excl-id", "POST", base + "/regles/retirer", url.Values{"kind": {kindExclusion}, "a": {"x"}}, zzError},
		{"retirer-excl", "POST", base + "/regles/retirer", url.Values{"kind": {kindExclusion}, "a": {A}, "b": {B}, "both": {"1"}}, zzRedirect},
		{"retirer-forcee-id", "POST", base + "/regles/retirer", url.Values{"kind": {kindForced}, "a": {"x"}}, zzError},
		{"retirer-forcee", "POST", base + "/regles/retirer", url.Values{"kind": {kindForced}, "a": {C}, "b": {D}}, zzRedirect},
		{"retirer-membre-id", "POST", base + "/regles/retirer", url.Values{"kind": {kindMember}, "a": {"x"}}, zzError},
		{"retirer-membre", "POST", base + "/regles/retirer", url.Values{"kind": {kindMember}, "a": {B}, "household": {"1"}}, zzRedirect},
		// Suppression.
		{"supprimer-id", "POST", base + "/abc/supprimer", nil, zz404},
		{"supprimer-inconnue", "POST", base + "/9999/supprimer", nil, zz404},
		{"supprimer-sans-confirmation", "POST", base + "/" + D + "/supprimer", nil, zzError},
		{"supprimer", "POST", base + "/" + D + "/supprimer", url.Values{"confirm": {"1"}}, zzRedirect},
	}
	for _, k := range calls {
		rec := new(struct{ code int })
		if k.method == "GET" {
			rec.code = e.get(t, k.path, "fr", cookies).Code
		} else {
			rec.code = e.post(t, k.path, k.form, "fr", cookies).Code
		}
		if !k.want(rec.code) {
			t.Errorf("%s : statut %d inattendu", k.name, rec.code)
		}
	}
	// Sans session administrateur : renvoi vers la connexion ; groupe inconnu : 404.
	if r := e.get(t, base, "fr", nil); r.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", r.Code)
	}
	if r := e.get(t, "/inconnu/admin/participants", "fr", nil); r.Code != http.StatusNotFound {
		t.Errorf("groupe inconnu : %d", r.Code)
	}
}

func TestZZPeopleAfterDrawAndFailures(t *testing.T) {
	e := newEnv(t)
	gid := e.groupA
	ids := []int64{
		e.person(t, gid, "Alice", "alice@example.org", participants.RoleNormal),
		e.person(t, gid, "Bruno", "bruno@example.org", participants.RoleNormal),
		e.person(t, gid, "Carla", "carla@example.org", participants.RoleNormal),
		e.person(t, gid, "Denis", "denis@example.org", participants.RoleNormal),
	}
	e.seedDraw(t, gid, ids)
	cookies := e.adminCookies(t, gid)
	base := "/noel/admin/participants"
	for _, form := range []url.Values{
		{"kind": {kindExclusion}, "a": {sid(ids[0])}, "b": {sid(ids[1])}},
		{"kind": {kindForced}, "a": {sid(ids[1])}, "b": {sid(ids[0])}},
	} {
		if r := e.post(t, base+"/regles", form, "fr", cookies); r.Code >= 500 {
			t.Errorf("règle après tirage : %d", r.Code)
		}
	}
	if r := e.get(t, base+"?supprimer="+sid(ids[0]), "fr", cookies); r.Code != 200 {
		t.Errorf("confirmation : %d", r.Code)
	}
	if r := e.get(t, base+"?q=Alice&tri=nom&role=normal", "fr", cookies); r.Code != 200 {
		t.Errorf("liste filtrée : %d", r.Code)
	}
	// Panne de base : la liste répond 500.
	if _, err := e.db.Exec(`DROP TABLE admin_audit`); err == nil {
		if r := e.post(t, base, url.Values{"first_name": {"Zoe"}, "name": {"Zed"}}, "fr", cookies); r.Code != http.StatusSeeOther {
			t.Errorf("journal en panne : %d", r.Code)
		}
	}
}
