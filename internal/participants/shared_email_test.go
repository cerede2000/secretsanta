package participants

import "testing"

func TestSharedEmail(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"Un", "Deux", "Trois"} {
		if _, err := e.svc.Create(t.Context(), e.admin(), e.g1, Input{FirstName: "P", Name: n, Email: "famille@example.org"}); err != nil {
			t.Fatalf("%s : %v", n, err)
		}
	}
	l, err := e.svc.List(t.Context(), e.admin(), e.g1)
	if err != nil || len(l) != 3 {
		t.Fatalf("liste : %v %v", l, err)
	}
}
