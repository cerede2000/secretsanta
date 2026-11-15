package ports

import (
	"context"
	"testing"
)

var _ SuppressionList = (*FakeSuppressionList)(nil)

func TestSuppressionPerGroup(t *testing.T) {
	ctx := context.Background()
	s := &FakeSuppressionList{}
	if err := s.Add(ctx, 1, "A@x.fr", "5.1.1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsSuppressed(ctx, 1, "a@x.fr"); !ok {
		t.Fatal("supprimée attendue pour le groupe 1")
	}
	if ok, _ := s.IsSuppressed(ctx, 2, "a@x.fr"); ok {
		t.Fatal("groupe 2 non concerné")
	}
	if err := s.Remove(ctx, 1, "a@x.fr"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsSuppressed(ctx, 1, "a@x.fr"); ok {
		t.Fatal("réactivée attendue")
	}
}
