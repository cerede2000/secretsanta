package invite

import (
	"context"
	"sort"
	"sync"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// Invitation est l'état du mode « inscription par lien » d'un groupe. Le
// jeton n'est jamais conservé en clair : seul son condensé HMAC est stocké.
type Invitation struct {
	GroupID int64
	// Enabled vaut false par défaut : le mode est désactivé.
	Enabled bool
	// TokenHash est le condensé HMAC-SHA-256 du jeton en vigueur (nil si
	// aucun jeton n'a jamais été émis).
	TokenHash []byte
	// Capacity est le plafond de places (participants et inscriptions en
	// attente non expirées) ; 0 = aucun plafond.
	Capacity int
	// ClosesAt est la date de clôture (UTC) ; nil = aucune.
	ClosesAt *time.Time
	// RoleChoice : « Proposer aux inscrits de choisir leur rôle » (F11d) ;
	// false par défaut, modifiable à tout moment par l'administrateur.
	RoleChoice bool
	UpdatedAt  time.Time
}

// Pending est une inscription en attente de vérification. Ce n'est pas un
// participant : aucun lien ni code personnel n'existe pour elle.
type Pending struct {
	ID      int64
	GroupID int64
	// Ref est une référence opaque et aléatoire de l'inscription, transmise à
	// l'étape du code ; elle n'est pas un secret d'accès (le code l'est).
	Ref       string
	FirstName string
	Name      string
	// NameKey est la clé d'unicité calculée par participants.NormalizeName.
	NameKey string
	Email   string
	// EmailKey est l'adresse en minuscules (comparaison sans casse).
	EmailKey string
	// SharedAddress vaut true si la personne a confirmé partager l'adresse.
	SharedAddress bool
	Lang          string
	// Role est le rôle choisi à l'inscription (F11d), transmis à
	// CreateVerified : normal, reception_seule ou offre_plus, jamais un_seul.
	// Il vaut normal quand l'option de choix est désactivée.
	Role      participants.DrawRole
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store persiste l'invitation et les inscriptions en attente. Toute lecture
// est bornée à un groupe. Les méthodes qui prennent now ignorent les
// inscriptions expirées (ExpiresAt <= now). Le magasin SQL (F11.6) doit
// respecter les mêmes contrats que MemoryStore.
type Store interface {
	// Invitation renvoie l'invitation du groupe ; sans ligne, une invitation
	// désactivée et vide (Enabled false, TokenHash nil).
	Invitation(ctx context.Context, groupID int64) (Invitation, error)
	SaveInvitation(ctx context.Context, inv Invitation) error

	// AddPending enregistre une inscription et renvoie sa copie avec ID.
	AddPending(ctx context.Context, p Pending) (Pending, error)
	DeletePending(ctx context.Context, groupID, id int64) error
	ListPending(ctx context.Context, groupID int64, now time.Time) ([]Pending, error)
	CountPending(ctx context.Context, groupID int64, now time.Time) (int, error)
	PendingNameExists(ctx context.Context, groupID int64, nameKey string, now time.Time) (bool, error)
	PendingEmailExists(ctx context.Context, groupID int64, emailKey string, now time.Time) (bool, error)
}

// MemoryStore est le magasin en mémoire, sûr en concurrence (tests et
// doublure de contrat du magasin SQL).
type MemoryStore struct {
	mu      sync.Mutex
	invites map[int64]Invitation
	pending map[int64]Pending
	nextID  int64
}

// NewMemoryStore crée un magasin vide.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{invites: map[int64]Invitation{}, pending: map[int64]Pending{}}
}

var _ Store = (*MemoryStore)(nil)

func cloneInvitation(inv Invitation) Invitation {
	inv.TokenHash = append([]byte(nil), inv.TokenHash...)
	if len(inv.TokenHash) == 0 {
		inv.TokenHash = nil
	}
	if inv.ClosesAt != nil {
		t := *inv.ClosesAt
		inv.ClosesAt = &t
	}
	return inv
}

// Invitation implémente Store.
func (m *MemoryStore) Invitation(_ context.Context, groupID int64) (Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.invites[groupID]
	if !ok {
		return Invitation{GroupID: groupID}, nil
	}
	return cloneInvitation(inv), nil
}

// SaveInvitation implémente Store.
func (m *MemoryStore) SaveInvitation(_ context.Context, inv Invitation) error {
	if inv.GroupID == 0 {
		return errs.Internal(nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invites[inv.GroupID] = cloneInvitation(inv)
	return nil
}

// AddPending implémente Store.
func (m *MemoryStore) AddPending(_ context.Context, p Pending) (Pending, error) {
	if p.GroupID == 0 {
		return Pending{}, errs.Internal(nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	p.ID = m.nextID
	m.pending[p.ID] = p
	return p, nil
}

// DeletePending implémente Store ; une inscription d'un autre groupe n'est
// jamais supprimée.
func (m *MemoryStore) DeletePending(_ context.Context, groupID, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pending[id]; ok && p.GroupID == groupID {
		delete(m.pending, id)
	}
	return nil
}

func (m *MemoryStore) live(groupID int64, now time.Time, keep func(Pending) bool) []Pending {
	var out []Pending
	for _, p := range m.pending {
		if p.GroupID == groupID && p.ExpiresAt.After(now) && keep(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ListPending implémente Store (ordre d'arrivée).
func (m *MemoryStore) ListPending(_ context.Context, groupID int64, now time.Time) ([]Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live(groupID, now, func(Pending) bool { return true }), nil
}

// CountPending implémente Store.
func (m *MemoryStore) CountPending(_ context.Context, groupID int64, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.live(groupID, now, func(Pending) bool { return true })), nil
}

// PendingNameExists implémente Store.
func (m *MemoryStore) PendingNameExists(_ context.Context, groupID int64, nameKey string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.live(groupID, now, func(p Pending) bool { return p.NameKey == nameKey })) > 0, nil
}

// PendingEmailExists implémente Store.
func (m *MemoryStore) PendingEmailExists(_ context.Context, groupID int64, emailKey string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.live(groupID, now, func(p Pending) bool { return p.EmailKey == emailKey })) > 0, nil
}
