package ports

import (
	"context"
	"sync"
)

// Event est un événement émis entre fonctionnalités.
type Event struct {
	Name    string
	GroupID int64
	Payload map[string]string
}

// EventHandler traite un événement.
type EventHandler func(ctx context.Context, e Event) error

// EventBus est le port d'événements (abonnement et émission).
type EventBus interface {
	Subscribe(name string, h EventHandler)
	Publish(ctx context.Context, e Event) error
}

// MemoryEventBus est une implémentation synchrone en mémoire, utilisable comme doublure.
type MemoryEventBus struct {
	mu       sync.Mutex
	handlers map[string][]EventHandler
	Emitted  []Event
}

// Subscribe abonne h aux événements de ce nom.
func (b *MemoryEventBus) Subscribe(name string, h EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handlers == nil {
		b.handlers = map[string][]EventHandler{}
	}
	b.handlers[name] = append(b.handlers[name], h)
}

// Publish émet l'événement à tous les abonnés, s'arrête à la première erreur.
func (b *MemoryEventBus) Publish(ctx context.Context, e Event) error {
	b.mu.Lock()
	b.Emitted = append(b.Emitted, e)
	hs := append([]EventHandler(nil), b.handlers[e.Name]...)
	b.mu.Unlock()
	for _, h := range hs {
		if err := h(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
