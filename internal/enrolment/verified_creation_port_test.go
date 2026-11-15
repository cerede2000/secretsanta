package enrolment

import (
	"strings"
	"testing"
	"time"

	"secretsanta/internal/access/sqlstore"
	"secretsanta/internal/access/token"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

func TestCreateVerified(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)
	c, err := e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "  Léa ", Name: "Martin"}, at)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.parts.Get(ctx, participants.AdminActor(e.gid), e.gid, c.Participant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.JoinedAt.Equal(at) {
		t.Fatalf("joined_at %v, attendu %v", p.JoinedAt, at)
	}
	if p.FirstName != "Léa" {
		t.Fatalf("nettoyage non appliqué : %q", p.FirstName)
	}
	if p.Role != participants.RoleNormal {
		t.Fatalf("rôle %q", p.Role)
	}
	if !strings.HasPrefix(c.Link, "https://exemple.test/noel/acces/") || c.Code == "" {
		t.Fatalf("lien/code absents : %q %q", c.Link, c.Code)
	}
	// Le lien et le code résolvent bien vers ce participant.
	tk, _ := token.NewService(sqlstore.New(e.db), token.LoginCodeLength, []byte("cle-de-test-0123456789-0123456789"))
	gid := "1"
	tok := c.Link[strings.LastIndex(c.Link, "/")+1:]
	if id, err := tk.ResolveLink(ctx, gid, tok); err != nil || id != "1" {
		t.Fatalf("lien : %v %q", err, id)
	}
	if id, err := tk.ResolveCode(ctx, gid, c.Code); err != nil || id != "1" {
		t.Fatalf("code : %v %q", err, id)
	}
	// Unicité insensible à la casse et aux accents.
	_, err = e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: "LEA", Name: "martin"}, at)
	if errs.KindOf(err) != errs.KindConflict {
		t.Fatalf("doublon accepté : %v", err)
	}
	// Longueur.
	_, err = e.svc.CreateVerified(ctx, e.gid, participants.Input{FirstName: strings.Repeat("a", 81), Name: "X"}, at)
	if err == nil {
		t.Fatal("prénom trop long accepté")
	}
}
