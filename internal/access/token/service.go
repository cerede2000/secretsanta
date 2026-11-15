package token

import (
	"context"
	"errors"
	"sync"

	"secretsanta/internal/shared/errs"
)

// Credential est un accès enregistré : seule l'empreinte du secret est
// conservée, liée au groupe et à la nature.
type Credential struct {
	GroupID       string
	ParticipantID string
	Kind          Kind
	Hash          string
}

// ErrDuplicate signale qu'une empreinte existe déjà dans le groupe pour cette
// nature (collision de code court).
var ErrDuplicate = errors.New("token: empreinte déjà présente dans le groupe")

// Store conserve les accès hachés.
//
// Put remplace l'accès existant du même participant pour la même nature
// (renouvellement : l'ancien secret cesse de fonctionner) et renvoie
// ErrDuplicate si l'empreinte appartient déjà à un autre participant du
// groupe. Find ne cherche que dans le groupe donné.
type Store interface {
	Put(ctx context.Context, c Credential) error
	Find(ctx context.Context, groupID string, kind Kind, hash string) (Credential, bool, error)
}

// maxCodeAttempts borne les tirages d'un code unique dans un groupe.
const maxCodeAttempts = 16

// Service délivre et vérifie les accès participant.
type Service struct {
	store      Store
	codeLength int
}

// NewService crée un service dont les codes courts font codeLength
// caractères (LoginCodeLength pour la connexion).
func NewService(store Store, codeLength int) (*Service, error) {
	if store == nil {
		return nil, errors.New("token: magasin absent")
	}
	if codeLength < MinCodeLength || codeLength > MaxCodeLength {
		return nil, ErrInvalidLength
	}
	return &Service{store: store, codeLength: codeLength}, nil
}

// errInvalidAccess est l'unique refus renvoyé, quelle qu'en soit la cause
// (groupe inconnu, secret mal formé, absent ou renouvelé).
func errInvalidAccess() *errs.Error {
	return errs.New(errs.KindUnauthorized, "Ce lien ou ce code n'est pas valide.")
}

func checkIDs(groupID, participantID string) error {
	if groupID == "" || participantID == "" {
		return errs.Validation("Groupe ou participant manquant.")
	}
	return nil
}

// IssueLink délivre un nouveau jeton de lien personnel pour le participant et
// renvoie le jeton en clair, à placer dans le lien remis ; il n'est pas
// conservé. L'ancien jeton du participant cesse de fonctionner.
func (s *Service) IssueLink(ctx context.Context, groupID, participantID string) (string, error) {
	if err := checkIDs(groupID, participantID); err != nil {
		return "", err
	}
	for i := 0; i < maxCodeAttempts; i++ {
		tok, err := NewToken()
		if err != nil {
			return "", errs.Internal(err)
		}
		err = s.store.Put(ctx, Credential{
			GroupID: groupID, ParticipantID: participantID,
			Kind: KindLink, Hash: Hash(groupID, KindLink, tok),
		})
		if errors.Is(err, ErrDuplicate) {
			continue
		}
		if err != nil {
			return "", errs.Internal(err)
		}
		return tok, nil
	}
	return "", errs.Internal(ErrDuplicate)
}

// IssueCode délivre un nouveau code court, unique dans le groupe, et le
// renvoie en clair ; il n'est pas conservé. L'ancien code du participant
// cesse de fonctionner.
func (s *Service) IssueCode(ctx context.Context, groupID, participantID string) (string, error) {
	if err := checkIDs(groupID, participantID); err != nil {
		return "", err
	}
	for i := 0; i < maxCodeAttempts; i++ {
		code, err := GenerateCode(s.codeLength)
		if err != nil {
			return "", errs.Internal(err)
		}
		err = s.store.Put(ctx, Credential{
			GroupID: groupID, ParticipantID: participantID,
			Kind: KindCode, Hash: Hash(groupID, KindCode, code),
		})
		if errors.Is(err, ErrDuplicate) {
			continue
		}
		if err != nil {
			return "", errs.Internal(err)
		}
		return code, nil
	}
	return "", errs.Internal(ErrDuplicate)
}

// ResolveLink renvoie le participant du groupe auquel appartient le jeton.
// Un jeton d'un autre groupe n'est jamais accepté.
func (s *Service) ResolveLink(ctx context.Context, groupID, tok string) (string, error) {
	return s.resolve(ctx, groupID, KindLink, tok, ValidToken(tok))
}

// ResolveCode renvoie le participant du groupe auquel appartient le code
// saisi (espaces, tirets et minuscules tolérés).
func (s *Service) ResolveCode(ctx context.Context, groupID, input string) (string, error) {
	code := NormalizeCode(input)
	return s.resolve(ctx, groupID, KindCode, code, ValidCode(code, s.codeLength))
}

func (s *Service) resolve(ctx context.Context, groupID string, kind Kind, secret string, wellFormed bool) (string, error) {
	// L'empreinte est calculée même pour une saisie mal formée afin de ne pas
	// distinguer les refus par leur durée.
	h := Hash(groupID, kind, secret)
	if groupID == "" || !wellFormed {
		return "", errInvalidAccess()
	}
	c, ok, err := s.store.Find(ctx, groupID, kind, h)
	if err != nil {
		return "", errs.Internal(err)
	}
	// Défense en profondeur : même si un magasin ignorait le groupe, l'accès
	// doit appartenir au groupe demandé et son empreinte coïncider.
	if !ok || c.GroupID != groupID || c.Kind != kind || !Equal(c.Hash, h) {
		return "", errInvalidAccess()
	}
	return c.ParticipantID, nil
}

// MemoryStore est un Store en mémoire, sûr en concurrence. Il sert aux tests
// et de référence au magasin persistant.
type MemoryStore struct {
	mu     sync.Mutex
	byHash map[memKey]Credential
	byUser map[memKey]string // (groupe, nature, participant) -> empreinte
}

type memKey struct {
	group, kind, value string
}

// NewMemoryStore crée un magasin vide.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byHash: map[memKey]Credential{}, byUser: map[memKey]string{}}
}

// Put implémente Store.
func (m *MemoryStore) Put(_ context.Context, c Credential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	hk := memKey{c.GroupID, string(c.Kind), c.Hash}
	if prev, ok := m.byHash[hk]; ok && prev.ParticipantID != c.ParticipantID {
		return ErrDuplicate
	}
	uk := memKey{c.GroupID, string(c.Kind), c.ParticipantID}
	if old, ok := m.byUser[uk]; ok {
		delete(m.byHash, memKey{c.GroupID, string(c.Kind), old})
	}
	m.byHash[hk] = c
	m.byUser[uk] = c.Hash
	return nil
}

// Find implémente Store.
func (m *MemoryStore) Find(_ context.Context, groupID string, kind Kind, hash string) (Credential, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byHash[memKey{groupID, string(kind), hash}]
	return c, ok, nil
}

// Len renvoie le nombre d'accès enregistrés.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byHash)
}
