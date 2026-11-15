package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrs(t *testing.T) {
	cause := errors.New("sql")
	e := fmt.Errorf("ctx: %w", Wrap(KindConflict, "Déjà pris.", cause))
	if KindOf(e) != KindConflict || !Is(e, KindConflict) || Is(e, KindNotFound) {
		t.Fatal("kind")
	}
	if !errors.Is(e, cause) {
		t.Fatal("unwrap")
	}
	if UserMessage(e) != "Déjà pris." {
		t.Fatal("msg")
	}
	if KindOf(cause) != KindInternal || UserMessage(cause) == "sql" {
		t.Fatal("untyped")
	}
}
