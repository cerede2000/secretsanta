package adminops_test

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"secretsanta/internal/shared/errs"
)

// F10b.AC6 : la réinitialisation génère un mot de passe affiché une seule
// fois (jamais stocké en clair) et invalide l'ancien.
func TestResetAccessGeneratesOneTimeSecretAndRevokesOld(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	g1, old := f.group(t, "famille", "Famille", "")
	g2, other := f.group(t, "amis", "Amis", "")
	p := f.person(t, g1, "Anne", "Leroy", "", "")
	f.seedTraces(t, g1, p)
	f.seedTraces(t, g2, f.person(t, g2, "Bob", "Roy", "", ""))

	secret, err := f.ops.ResetAdminSecret(ctx, g1)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < 22 || secret == old {
		t.Fatalf("nouveau secret %q", secret)
	}
	if _, err := f.groups.VerifyAdminSecret(ctx, "famille", old); !errs.Is(err, errs.KindUnauthorized) {
		t.Errorf("l'ancien secret fonctionne encore : %v", err)
	}
	if id, err := f.groups.VerifyAdminSecret(ctx, "famille", secret); err != nil || id != g1 {
		t.Errorf("le nouveau secret ne fonctionne pas : %v", err)
	}
	// Les sessions administrateur du groupe sont fermées, pas celles des autres.
	if n := f.count(t, `SELECT COUNT(*) FROM websec_sessions WHERE group_id = ?`, strconv.FormatInt(g1, 10)); n != 0 {
		t.Errorf("%d sessions restent ouvertes", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM websec_sessions WHERE group_id = ?`, strconv.FormatInt(g2, 10)); n != 1 {
		t.Errorf("les sessions de l'autre groupe ont été touchées : %d", n)
	}
	if id, err := f.groups.VerifyAdminSecret(ctx, "amis", other); err != nil || id != g2 {
		t.Errorf("l'autre groupe a perdu son secret : %v", err)
	}
	// Affiché une seule fois : le secret n'apparaît nulle part dans la base
	// (ni en clair ni dans une sauvegarde), et un second appel en donne un autre.
	var dump bytes.Buffer
	if err := f.ops.Backup(ctx, g1, &dump); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(dump.Bytes(), []byte(secret)) {
		t.Error("le secret est stocké en clair")
	}
	again, err := f.ops.ResetAdminSecret(ctx, g1)
	if err != nil || again == secret {
		t.Fatalf("second secret %q : %v", again, err)
	}
	if _, err := f.groups.VerifyAdminSecret(ctx, "famille", secret); err == nil {
		t.Error("le secret précédent fonctionne encore")
	}
	if _, err := f.ops.ResetAdminSecret(ctx, 9999); !errs.Is(err, errs.KindNotFound) {
		t.Errorf("groupe inconnu : %v", err)
	}
}
