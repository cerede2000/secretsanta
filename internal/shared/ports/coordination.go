package ports

import (
	"context"
	"sync"
)

// RegistrationResolution est la façon de résoudre des inscriptions en attente.
type RegistrationResolution string

const (
	// ResolveIgnore ignore les inscriptions en attente.
	ResolveIgnore RegistrationResolution = "ignorer"
	// ResolveDelete supprime les inscriptions en attente.
	ResolveDelete RegistrationResolution = "supprimer"
)

// RegistrationState expose les inscriptions en attente d'un groupe (implémenté par F11).
type RegistrationState interface {
	PendingCount(ctx context.Context, groupID int64) (int, error)
	Resolve(ctx context.Context, groupID int64, how RegistrationResolution) error
}

// DrawState indique si le tirage d'un groupe est fait (implémenté par F4).
type DrawState interface {
	IsDrawDone(ctx context.Context, groupID int64) (bool, error)
}

// FakeRegistrationState est une doublure de RegistrationState.
type FakeRegistrationState struct {
	mu       sync.Mutex
	Pending  map[int64]int
	Resolved map[int64]RegistrationResolution
}

// PendingCount renvoie 0 pour un groupe inconnu.
func (f *FakeRegistrationState) PendingCount(_ context.Context, groupID int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Pending[groupID], nil
}

// Resolve résout les inscriptions en attente et mémorise le choix.
func (f *FakeRegistrationState) Resolve(_ context.Context, groupID int64, how RegistrationResolution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Resolved == nil {
		f.Resolved = map[int64]RegistrationResolution{}
	}
	f.Resolved[groupID] = how
	delete(f.Pending, groupID)
	return nil
}

// FakeDrawState est une doublure de DrawState.
type FakeDrawState struct {
	mu   sync.Mutex
	Done map[int64]bool
}

// IsDrawDone renvoie false pour un groupe inconnu.
func (f *FakeDrawState) IsDrawDone(_ context.Context, groupID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Done[groupID], nil
}
