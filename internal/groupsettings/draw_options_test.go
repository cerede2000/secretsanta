package groupsettings

import (
	"context"
	"testing"

	"secretsanta/internal/shared/errs"
)

// Les options du tirage sont désactivées par défaut, relues telles
// qu'enregistrées, propres à chaque groupe, et un groupe inconnu est refusé.
func TestDrawOptionsDefaultsAndRoundTrip(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if o, err := e.svc.GetDrawOptions(ctx, e.gid); err != nil || o != (DrawOptions{}) {
		t.Fatalf("défaut : %+v %v", o, err)
	}
	if err := e.svc.SetDrawOptions(ctx, e.gid, DrawOptions{ExcludePreviousYear: true}); err != nil {
		t.Fatal(err)
	}
	ex, sw, err := e.svc.DrawOptions(ctx, e.gid)
	if err != nil || !ex || sw {
		t.Fatalf("port après exclusion seule : %v %v %v", ex, sw, err)
	}
	if err := e.svc.SetDrawOptions(ctx, e.gid, DrawOptions{AntiSwap: true}); err != nil {
		t.Fatal(err)
	}
	if o, _ := e.svc.GetDrawOptions(ctx, e.gid); o != (DrawOptions{AntiSwap: true}) {
		t.Fatalf("le second enregistrement doit remplacer le premier : %+v", o)
	}
	if err := e.svc.SetDrawOptions(ctx, e.gid+99, DrawOptions{AntiSwap: true}); !errs.Is(err, errs.KindNotFound) {
		t.Fatalf("groupe inconnu : %v", err)
	}
}
