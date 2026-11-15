package verify

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Outcome est la trace d'une inscription vérifiée (par code, par lien en un
// clic ou par l'administrateur). Elle rend la vérification idempotente : une
// personne qui saisit son code après une validation par l'administrateur
// retombe sur l'écran de réussite, sans second participant. Elle ne vit que
// jusqu'à l'expiration de l'inscription (ExpiresAt), puis elle est purgée.
type Outcome struct {
	GroupID        int64
	RegistrationID int64
	// Ref est la référence opaque de l'inscription (invite.Pending.Ref).
	Ref           string
	ParticipantID int64
	VerifiedAt    time.Time
	// ByAdmin est vrai pour « Valider sans code ».
	ByAdmin bool
	// ExpiresAt est l'expiration de l'inscription d'origine.
	ExpiresAt time.Time
}

// Action est une action de l'administrateur sur une inscription.
type Action string

// Actions journalisées.
const (
	ActionValidate Action = "valider_sans_code"
	ActionResend   Action = "renvoyer_code"
	ActionDelete   Action = "supprimer"
)

// AuditEntry est une ligne du journal des actions de l'administrateur sur
// les inscriptions : qui (l'administrateur du groupe), quand, quelle
// inscription. Elle ne contient jamais de paire.
type AuditEntry struct {
	GroupID        int64
	RegistrationID int64
	Action         Action
	// Actor est la nature de l'auteur (toujours « administrateur » ici).
	Actor string
	At    time.Time
	// FirstName et Name rappellent l'inscription concernée.
	FirstName string
	Name      string
	// ParticipantID est le participant créé (ActionValidate), sinon 0.
	ParticipantID int64
}

// Store conserve l'état propre à la vérification : traces des inscriptions
// vérifiées, inscriptions « ignorées » et journal des actions de
// l'administrateur. Toute opération est bornée au groupe donné. Les
// inscriptions elles-mêmes restent dans invite.Store.
type Store interface {
	SaveOutcome(ctx context.Context, o Outcome) error
	// Outcome renvoie la trace de l'inscription si elle existe.
	Outcome(ctx context.Context, groupID, registrationID int64) (Outcome, bool, error)
	// OutcomeByRef renvoie la trace dont la référence vaut ref.
	OutcomeByRef(ctx context.Context, groupID int64, ref string) (Outcome, bool, error)
	DeleteOutcome(ctx context.Context, groupID, registrationID int64) error
	// DeleteOutcomesExpired retire les traces dont ExpiresAt <= now.
	DeleteOutcomesExpired(ctx context.Context, groupID int64, now time.Time) (int, error)

	// SetIgnored marque l'inscription « ignorée » (fermée, sans code).
	SetIgnored(ctx context.Context, groupID, registrationID int64, at time.Time) error
	// Ignored renvoie les identifiants des inscriptions ignorées du groupe.
	Ignored(ctx context.Context, groupID int64) (map[int64]bool, error)
	ClearIgnored(ctx context.Context, groupID, registrationID int64) error

	AppendAudit(ctx context.Context, e AuditEntry) error
	// Audit renvoie le journal du groupe, du plus ancien au plus récent.
	Audit(ctx context.Context, groupID int64) ([]AuditEntry, error)
}

// MemoryStore est un Store en mémoire, sûr en concurrence (tests et
// référence du magasin SQL).
type MemoryStore struct {
	mu       sync.Mutex
	outcomes map[key]Outcome
	ignored  map[key]time.Time
	audit    []AuditEntry
}

type key struct{ group, registration int64 }

// NewMemoryStore crée un magasin vide.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{outcomes: map[key]Outcome{}, ignored: map[key]time.Time{}}
}

var _ Store = (*MemoryStore)(nil)

// SaveOutcome implémente Store.
func (m *MemoryStore) SaveOutcome(_ context.Context, o Outcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes[key{o.GroupID, o.RegistrationID}] = o
	return nil
}

// Outcome implémente Store.
func (m *MemoryStore) Outcome(_ context.Context, groupID, registrationID int64) (Outcome, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.outcomes[key{groupID, registrationID}]
	return o, ok, nil
}

// OutcomeByRef implémente Store.
func (m *MemoryStore) OutcomeByRef(_ context.Context, groupID int64, ref string) (Outcome, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ref == "" {
		return Outcome{}, false, nil
	}
	for k, o := range m.outcomes {
		if k.group == groupID && o.Ref == ref {
			return o, true, nil
		}
	}
	return Outcome{}, false, nil
}

// DeleteOutcome implémente Store.
func (m *MemoryStore) DeleteOutcome(_ context.Context, groupID, registrationID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.outcomes, key{groupID, registrationID})
	return nil
}

// DeleteOutcomesExpired implémente Store.
func (m *MemoryStore) DeleteOutcomesExpired(_ context.Context, groupID int64, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, o := range m.outcomes {
		if k.group == groupID && !o.ExpiresAt.After(now) {
			delete(m.outcomes, k)
			n++
		}
	}
	return n, nil
}

// SetIgnored implémente Store.
func (m *MemoryStore) SetIgnored(_ context.Context, groupID, registrationID int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ignored[key{groupID, registrationID}] = at
	return nil
}

// Ignored implémente Store.
func (m *MemoryStore) Ignored(_ context.Context, groupID int64) (map[int64]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]bool{}
	for k := range m.ignored {
		if k.group == groupID {
			out[k.registration] = true
		}
	}
	return out, nil
}

// ClearIgnored implémente Store.
func (m *MemoryStore) ClearIgnored(_ context.Context, groupID, registrationID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ignored, key{groupID, registrationID})
	return nil
}

// AppendAudit implémente Store.
func (m *MemoryStore) AppendAudit(_ context.Context, e AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, e)
	return nil
}

// Audit implémente Store.
func (m *MemoryStore) Audit(_ context.Context, groupID int64) ([]AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AuditEntry
	for _, e := range m.audit {
		if e.GroupID == groupID {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}
