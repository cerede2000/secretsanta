package code

import (
	"context"
	"sync"
	"time"
)

// Record est le code en cours d'une inscription en attente. Seules les
// empreintes du code et du jeton de confirmation sont conservées.
type Record struct {
	GroupID        int64
	RegistrationID int64
	// CodeHash est l'empreinte à clé du code (liée au groupe et à
	// l'inscription) ; TokenHash celle du jeton de confirmation.
	CodeHash  string
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
	// Attempts compte les saisies du code (y compris la bonne).
	Attempts int
	// Consumed est vrai une fois le code ou le jeton utilisé avec succès.
	Consumed bool
}

// Store conserve les codes de vérification. Une inscription a au plus un code
// en cours. Toutes les opérations sont limitées au groupe donné.
//
// Les opérations de comptage et de consommation sont atomiques et
// conditionnées par l'empreinte du code : un code remplacé entre la lecture
// et l'écriture n'est jamais modifié.
type Store interface {
	// Put remplace le code de l'inscription (l'ancien code et l'ancien jeton
	// cessent de fonctionner).
	Put(ctx context.Context, r Record) error
	// Get renvoie le code en cours de l'inscription.
	Get(ctx context.Context, groupID, registrationID int64) (Record, bool, error)
	// FindByToken renvoie le code dont le jeton de confirmation a cette
	// empreinte, dans ce groupe seulement.
	FindByToken(ctx context.Context, groupID int64, tokenHash string) (Record, bool, error)
	// AddAttempt incrémente le compteur de saisies du code d'empreinte
	// codeHash et renvoie la nouvelle valeur ; ok est faux si ce code n'est
	// plus celui de l'inscription.
	AddAttempt(ctx context.Context, groupID, registrationID int64, codeHash string) (attempts int, ok bool, err error)
	// Consume marque utilisé le code d'empreinte codeHash si, au même instant,
	// il n'est pas déjà utilisé, n'a pas expiré (now < ExpiresAt) et compte au
	// plus maxAttempts saisies. Il rapporte si cet appel l'a consommé.
	Consume(ctx context.Context, groupID, registrationID int64, codeHash string, now time.Time, maxAttempts int) (bool, error)
	// Delete retire le code de l'inscription (sans erreur s'il n'existe pas).
	Delete(ctx context.Context, groupID, registrationID int64) error
}

// MemoryStore est un Store en mémoire, sûr en concurrence. Il sert aux tests
// et de référence au magasin SQL.
type MemoryStore struct {
	mu      sync.Mutex
	records map[memKey]Record
}

type memKey struct{ group, registration int64 }

// NewMemoryStore crée un magasin vide.
func NewMemoryStore() *MemoryStore { return &MemoryStore{records: map[memKey]Record{}} }

var _ Store = (*MemoryStore)(nil)

// Put implémente Store.
func (m *MemoryStore) Put(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[memKey{r.GroupID, r.RegistrationID}] = r
	return nil
}

// Get implémente Store.
func (m *MemoryStore) Get(_ context.Context, groupID, registrationID int64) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[memKey{groupID, registrationID}]
	return r, ok, nil
}

// FindByToken implémente Store.
func (m *MemoryStore) FindByToken(_ context.Context, groupID int64, tokenHash string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.records {
		if k.group == groupID && r.TokenHash == tokenHash {
			return r, true, nil
		}
	}
	return Record{}, false, nil
}

// AddAttempt implémente Store.
func (m *MemoryStore) AddAttempt(_ context.Context, groupID, registrationID int64, codeHash string) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := memKey{groupID, registrationID}
	r, ok := m.records[k]
	if !ok || r.CodeHash != codeHash {
		return 0, false, nil
	}
	r.Attempts++
	m.records[k] = r
	return r.Attempts, true, nil
}

// Consume implémente Store.
func (m *MemoryStore) Consume(_ context.Context, groupID, registrationID int64, codeHash string, now time.Time, maxAttempts int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := memKey{groupID, registrationID}
	r, ok := m.records[k]
	if !ok || r.CodeHash != codeHash || r.Consumed || !now.Before(r.ExpiresAt) || r.Attempts > maxAttempts {
		return false, nil
	}
	r.Consumed = true
	m.records[k] = r
	return true, nil
}

// Delete implémente Store.
func (m *MemoryStore) Delete(_ context.Context, groupID, registrationID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, memKey{groupID, registrationID})
	return nil
}

// Records renvoie une copie de tous les codes (pour les tests).
func (m *MemoryStore) Records() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out
}
