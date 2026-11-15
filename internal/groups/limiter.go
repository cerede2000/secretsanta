package groups

import (
	"sync"
	"time"

	"secretsanta/internal/shared/clock"
)

// Limitation des tentatives de mot de passe d'instance.
const (
	MaxPasswordFailures = 5
	PasswordFailWindow  = 15 * time.Minute
)

// attemptLimiter compte les échecs par clé de client (adresse IP fournie par
// l'appelant) sur une fenêtre glissante simple : au-delà de max échecs, la
// clé est bloquée jusqu'à l'expiration de la fenêtre ouverte au premier
// échec.
type attemptLimiter struct {
	mu      sync.Mutex
	clock   clock.Clock
	max     int
	window  time.Duration
	entries map[string]*attemptEntry
}

type attemptEntry struct {
	failures int
	start    time.Time
}

func newAttemptLimiter(c clock.Clock, max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{clock: c, max: max, window: window, entries: map[string]*attemptEntry{}}
}

func (l *attemptLimiter) pruneLocked(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.start) >= l.window {
			delete(l.entries, k)
		}
	}
}

// blocked indique si la clé a épuisé ses tentatives.
func (l *attemptLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.pruneLocked(now)
	e, ok := l.entries[key]
	return ok && e.failures >= l.max
}

// fail enregistre un échec.
func (l *attemptLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.start) >= l.window {
		e = &attemptEntry{start: now}
		l.entries[key] = e
	}
	e.failures++
}
