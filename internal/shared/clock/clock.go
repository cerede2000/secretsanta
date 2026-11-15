// Package clock fournit l'horloge réelle et l'horloge simulée.
package clock

import (
	"sync"
	"time"
)

// Clock donne l'heure courante.
type Clock interface {
	Now() time.Time
}

// Real est l'horloge système.
type Real struct{}

// Now renvoie l'heure système en UTC.
func (Real) Now() time.Time { return time.Now().UTC() }

// Fake est une horloge simulée, pilotée par les tests.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake crée une horloge simulée à l'instant donné.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now renvoie l'instant simulé.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set fixe l'instant simulé.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// Advance avance l'instant simulé de d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
