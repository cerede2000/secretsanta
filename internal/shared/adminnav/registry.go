// Package adminnav est le registre de navigation d'administration.
package adminnav

import (
	"sort"
	"sync"
)

// Rangs canoniques : Tableau, Participants, Tirage, Inscriptions, Plus.
const (
	RankDashboard     = 10
	RankParticipants  = 20
	RankDraw          = 30
	RankRegistrations = 40
	RankMore          = 50
)

// Entry est une entrée déclarée par une fonctionnalité.
type Entry struct {
	Key         string
	Label       string
	Icon        string
	Rank        int
	Destination string
}

// Item est une entrée résolue, avec son état actif.
type Item struct {
	Entry
	Active bool
}

// Registry collecte les entrées.
type Registry struct {
	mu      sync.Mutex
	entries map[string]Entry
}

// New crée un registre vide.
func New() *Registry { return &Registry{entries: map[string]Entry{}} }

// Declare ajoute ou remplace l'entrée portant la même clé.
func (r *Registry) Declare(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[e.Key] = e
}

// Items renvoie les entrées triées par rang (puis clé), activeKey étant marquée active.
func (r *Registry) Items(activeKey string) []Item {
	r.mu.Lock()
	out := make([]Item, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, Item{Entry: e, Active: e.Key == activeKey})
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].Key < out[j].Key
	})
	return out
}
