package token

import (
	"context"
	"testing"

	"secretsanta/internal/shared/errs"

	"pgregory.net/rapid"
)

// Propriété : un jeton ou un code délivré dans un groupe ne donne jamais
// accès à un autre groupe, même quand les identifiants de participants se
// recoupent.
func TestGroupIsolationProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		groupA := rapid.StringMatching(`[a-z0-9-]{1,12}`).Draw(t, "groupA")
		groupB := rapid.StringMatching(`[a-z0-9-]{1,12}`).Filter(func(s string) bool { return s != groupA }).Draw(t, "groupB")
		participant := rapid.StringMatching(`[a-z0-9]{1,8}`).Draw(t, "participant")

		svc, _ := NewService(NewMemoryStore(), LoginCodeLength)
		ctx := context.Background()
		tokA, err := svc.IssueLink(ctx, groupA, participant)
		if err != nil {
			t.Fatal(err)
		}
		codeA, err := svc.IssueCode(ctx, groupA, participant)
		if err != nil {
			t.Fatal(err)
		}
		// Le même participant existe aussi dans le groupe B.
		if _, err := svc.IssueLink(ctx, groupB, participant); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.IssueCode(ctx, groupB, participant); err != nil {
			t.Fatal(err)
		}

		if p, err := svc.ResolveLink(ctx, groupB, tokA); err == nil {
			t.Fatalf("jeton du groupe %q accepté dans %q (participant %q)", groupA, groupB, p)
		} else if !errs.Is(err, errs.KindUnauthorized) {
			t.Fatalf("refus inattendu : %v", err)
		}
		if p, err := svc.ResolveCode(ctx, groupB, codeA); err == nil {
			t.Fatalf("code du groupe %q accepté dans %q (participant %q)", groupA, groupB, p)
		}
		if p, err := svc.ResolveLink(ctx, groupA, tokA); err != nil || p != participant {
			t.Fatalf("jeton refusé dans son propre groupe : %q, %v", p, err)
		}
		if p, err := svc.ResolveCode(ctx, groupA, codeA); err != nil || p != participant {
			t.Fatalf("code refusé dans son propre groupe : %q, %v", p, err)
		}
	})
}

// Propriété : l'empreinte est liée au groupe ; le même secret a deux
// empreintes différentes dans deux groupes, y compris pour des identifiants
// qui ne diffèrent que par leur découpage.
func TestHashBoundToGroupProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.String().Draw(t, "a")
		b := rapid.String().Filter(func(s string) bool { return s != a }).Draw(t, "b")
		secret := rapid.String().Draw(t, "secret")
		if Hash(a, KindLink, secret) == Hash(b, KindLink, secret) {
			t.Fatalf("même empreinte pour les groupes %q et %q", a, b)
		}
	})
	// Découpage ambigu : ("ab","c") et ("a","bc") ne doivent pas coïncider.
	if Hash("ab", KindLink, "c") == Hash("a", KindLink, "bc") {
		t.Fatal("concaténation ambiguë dans l'empreinte")
	}
}

// Un même code peut exister dans deux groupes (l'unicité est par groupe) sans
// qu'aucun des deux ne mène à l'autre.
func TestSameCodeInTwoGroupsStaysIsolated(t *testing.T) {
	st := NewMemoryStore()
	svc, _ := NewService(st, LoginCodeLength)
	ctx := context.Background()
	code := "ABCD2345"
	for _, c := range []Credential{
		{GroupID: "g1", ParticipantID: "alice", Kind: KindCode, Hash: Hash("g1", KindCode, code)},
		{GroupID: "g2", ParticipantID: "bob", Kind: KindCode, Hash: Hash("g2", KindCode, code)},
	} {
		if err := st.Put(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := svc.ResolveCode(ctx, "g1", code); err != nil || p != "alice" {
		t.Fatalf("g1 : %q, %v", p, err)
	}
	if p, err := svc.ResolveCode(ctx, "g2", code); err != nil || p != "bob" {
		t.Fatalf("g2 : %q, %v", p, err)
	}
}

// leakyStore ignore le groupe à la recherche (défaut d'un magasin) : la
// liaison de l'empreinte au groupe suffit à empêcher l'accès croisé.
type leakyStore struct{ all []Credential }

func (l *leakyStore) Put(_ context.Context, c Credential) error {
	l.all = append(l.all, c)
	return nil
}

func (l *leakyStore) Find(_ context.Context, _ string, kind Kind, hash string) (Credential, bool, error) {
	for _, c := range l.all {
		if c.Kind == kind && c.Hash == hash {
			return c, true, nil
		}
	}
	return Credential{}, false, nil
}

// promiscuousStore renvoie le premier accès venu : le service doit vérifier
// lui-même le groupe et l'empreinte.
type promiscuousStore struct{ leakyStore }

func (p *promiscuousStore) Find(context.Context, string, Kind, string) (Credential, bool, error) {
	if len(p.all) == 0 {
		return Credential{}, false, nil
	}
	return p.all[0], true, nil
}

func TestIsolationSurvivesFaultyStore(t *testing.T) {
	ctx := context.Background()
	for name, st := range map[string]Store{"leaky": &leakyStore{}, "promiscuous": &promiscuousStore{}} {
		svc, _ := NewService(st, LoginCodeLength)
		tok, err := svc.IssueLink(ctx, "g1", "alice")
		if err != nil {
			t.Fatal(err)
		}
		code, err := svc.IssueCode(ctx, "g1", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if p, err := svc.ResolveLink(ctx, "g2", tok); err == nil {
			t.Errorf("%s : jeton de g1 accepté dans g2 (%q)", name, p)
		}
		if p, err := svc.ResolveCode(ctx, "g2", code); err == nil {
			t.Errorf("%s : code de g1 accepté dans g2 (%q)", name, p)
		}
	}
}

// Tous les refus se ressemblent : groupe inconnu, groupe vide, secret mal
// formé ou absent donnent la même erreur et le même message.
func TestUniformRefusal(t *testing.T) {
	svc, _ := NewService(NewMemoryStore(), LoginCodeLength)
	ctx := context.Background()
	tok, _ := svc.IssueLink(ctx, "g1", "alice")
	other, _ := NewToken()
	var msgs []string
	for _, tc := range []struct{ group, tok string }{
		{"g2", tok}, {"", tok}, {"g1", "pas-un-jeton"}, {"g1", other}, {"g1", ""},
	} {
		_, err := svc.ResolveLink(ctx, tc.group, tc.tok)
		if !errs.Is(err, errs.KindUnauthorized) {
			t.Fatalf("%+v : erreur %v", tc, err)
		}
		msgs = append(msgs, errs.UserMessage(err))
	}
	for _, input := range []string{"", "0OIL1234", "ABC", "ABCD2345"} {
		_, err := svc.ResolveCode(ctx, "g1", input)
		if !errs.Is(err, errs.KindUnauthorized) {
			t.Fatalf("code %q : erreur %v", input, err)
		}
		msgs = append(msgs, errs.UserMessage(err))
	}
	for _, m := range msgs {
		if m != msgs[0] {
			t.Fatalf("messages de refus différents : %q", msgs)
		}
	}
}

func TestIssueRequiresIDs(t *testing.T) {
	svc, _ := NewService(NewMemoryStore(), LoginCodeLength)
	ctx := context.Background()
	if _, err := svc.IssueLink(ctx, "", "p"); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("groupe vide : %v", err)
	}
	if _, err := svc.IssueCode(ctx, "g", ""); !errs.Is(err, errs.KindValidation) {
		t.Fatalf("participant vide : %v", err)
	}
}

// corruptStore renvoie un accès dont l'empreinte correspond mais dont le
// groupe enregistré est un autre (ligne corrompue) : le service refuse.
type corruptStore struct{}

func (corruptStore) Put(context.Context, Credential) error { return nil }

func (corruptStore) Find(_ context.Context, groupID string, kind Kind, hash string) (Credential, bool, error) {
	return Credential{GroupID: groupID + "-autre", ParticipantID: "intrus", Kind: kind, Hash: hash}, true, nil
}

func TestResolveChecksRecordedGroup(t *testing.T) {
	svc, _ := NewService(corruptStore{}, LoginCodeLength)
	tok, _ := NewToken()
	if p, err := svc.ResolveLink(context.Background(), "g1", tok); err == nil {
		t.Fatalf("accès d'un autre groupe accepté (%q)", p)
	}
}
