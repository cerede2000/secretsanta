// Package scheduler fournit un planificateur par « tick » piloté par une horloge.
package scheduler

import (
	"context"
	"sort"
	"sync"
	"time"

	"secretsanta/internal/shared/clock"
)

// Task est une tâche exécutée à l'instant voulu.
type Task func(ctx context.Context) error

type entry struct {
	id  uint64
	at  time.Time
	run Task
}

// Scheduler exécute chaque tâche une seule fois, au premier tick où l'horloge
// a atteint ou dépassé son instant.
type Scheduler struct {
	clk     clock.Clock
	mu      sync.Mutex
	next    uint64
	pending []entry
}

// New crée un planificateur lié à l'horloge.
func New(clk clock.Clock) *Scheduler { return &Scheduler{clk: clk} }

// At planifie t à l'instant at et renvoie un identifiant d'annulation.
func (s *Scheduler) At(at time.Time, t Task) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.pending = append(s.pending, entry{id: s.next, at: at, run: t})
	return s.next
}

// Cancel retire une tâche en attente ; renvoie false si elle est inconnue.
func (s *Scheduler) Cancel(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.pending {
		if e.id == id {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			return true
		}
	}
	return false
}

// Pending renvoie le nombre de tâches en attente.
func (s *Scheduler) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// Tick exécute, dans l'ordre chronologique, les tâches échues et renvoie le
// nombre exécuté et la première erreur rencontrée (les autres tâches
// s'exécutent quand même).
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	now := s.clk.Now()
	s.mu.Lock()
	var due, rest []entry
	for _, e := range s.pending {
		if !e.at.After(now) {
			due = append(due, e)
		} else {
			rest = append(rest, e)
		}
	}
	s.pending = rest
	s.mu.Unlock()
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	var first error
	for _, e := range due {
		if err := e.run(ctx); err != nil && first == nil {
			first = err
		}
	}
	return len(due), first
}

// Run appelle Tick à chaque période jusqu'à l'annulation du contexte.
func (s *Scheduler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = s.Tick(ctx)
		}
	}
}
