package ports

import (
	"context"
	"strings"
	"sync"
)

// SuppressionList liste les adresses refusées définitivement (SMTP 5xx), par groupe.
type SuppressionList interface {
	IsSuppressed(ctx context.Context, groupID int64, address string) (bool, error)
	Add(ctx context.Context, groupID int64, address, reason string) error
	Remove(ctx context.Context, groupID int64, address string) error
}

type suppressionKey struct {
	group   int64
	address string
}

// FakeSuppressionList est une doublure en mémoire.
type FakeSuppressionList struct {
	mu      sync.Mutex
	entries map[suppressionKey]string
}

func normAddr(a string) string { return strings.ToLower(strings.TrimSpace(a)) }

// IsSuppressed indique si l'adresse est supprimée pour ce groupe seulement.
func (f *FakeSuppressionList) IsSuppressed(_ context.Context, groupID int64, address string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.entries[suppressionKey{groupID, normAddr(address)}]
	return ok, nil
}

// Add inscrit l'adresse pour ce groupe.
func (f *FakeSuppressionList) Add(_ context.Context, groupID int64, address, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.entries == nil {
		f.entries = map[suppressionKey]string{}
	}
	f.entries[suppressionKey{groupID, normAddr(address)}] = reason
	return nil
}

// Remove réactive l'adresse pour ce groupe.
func (f *FakeSuppressionList) Remove(_ context.Context, groupID int64, address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, suppressionKey{groupID, normAddr(address)})
	return nil
}
