// Package sqlstore est le magasin SQLite de l'inscription par lien (F11.6).
//
// InviteStore implémente invite.Store (invitation et inscriptions en
// attente) et CodeStore implémente code.Store (codes de vérification), sur
// les tables créées par internal/registration/migrations. Les deux magasins
// respectent les contrats des doublures en mémoire invite.MemoryStore et
// code.MemoryStore.
//
// Seules des empreintes sont transmises et conservées : le jeton
// d'invitation, le code de vérification et le jeton de confirmation en un
// clic n'atteignent jamais la base en clair. Les instants sont stockés en
// nanosecondes Unix et relus en UTC.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/shared/errs"
)

func toNanos(t time.Time) int64 { return t.UTC().UnixNano() }

func fromNanos(n int64) time.Time { return time.Unix(0, n).UTC() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// InviteStore implémente invite.Store sur une base SQLite migrée.
type InviteStore struct {
	db *sql.DB
}

var _ invite.Store = (*InviteStore)(nil)

// NewInviteStore renvoie le magasin de l'invitation et des inscriptions en
// attente ; d doit avoir reçu les migrations de l'inscription.
func NewInviteStore(d *sql.DB) *InviteStore { return &InviteStore{db: d} }

// Invitation implémente invite.Store : sans ligne, une invitation désactivée
// et vide.
func (s *InviteStore) Invitation(ctx context.Context, groupID int64) (invite.Invitation, error) {
	var (
		inv      = invite.Invitation{GroupID: groupID}
		enabled  int
		hash     []byte
		closesAt sql.NullInt64
		updated  int64
		// roleChoice est l'option « choix du rôle » (F11d).
		roleChoice int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled, token_hash, capacity, closes_at, role_choice, updated_at
		 FROM registration_invitations WHERE group_id = ?`, groupID).
		Scan(&enabled, &hash, &inv.Capacity, &closesAt, &roleChoice, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return inv, nil
	}
	if err != nil {
		return invite.Invitation{}, errs.Internal(err)
	}
	inv.Enabled = enabled == 1
	inv.RoleChoice = roleChoice == 1
	if len(hash) > 0 {
		inv.TokenHash = append([]byte(nil), hash...)
	}
	if closesAt.Valid {
		t := fromNanos(closesAt.Int64)
		inv.ClosesAt = &t
	}
	inv.UpdatedAt = fromNanos(updated)
	return inv, nil
}

// SaveInvitation implémente invite.Store (création ou remplacement complet).
// Le condensé précédent est écrasé : l'ancien jeton cesse aussitôt de
// correspondre.
func (s *InviteStore) SaveInvitation(ctx context.Context, inv invite.Invitation) error {
	if inv.GroupID == 0 {
		return errs.Internal(nil)
	}
	var hash any
	if len(inv.TokenHash) > 0 {
		hash = append([]byte(nil), inv.TokenHash...)
	}
	var closesAt any
	if inv.ClosesAt != nil {
		closesAt = toNanos(*inv.ClosesAt)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registration_invitations (group_id, enabled, token_hash, capacity, closes_at, role_choice, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (group_id) DO UPDATE SET
			enabled = excluded.enabled, token_hash = excluded.token_hash,
			capacity = excluded.capacity, closes_at = excluded.closes_at,
			role_choice = excluded.role_choice, updated_at = excluded.updated_at`,
		inv.GroupID, boolInt(inv.Enabled), hash, inv.Capacity, closesAt, boolInt(inv.RoleChoice), toNanos(inv.UpdatedAt))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// AddPending implémente invite.Store et renvoie la copie avec son ID. Un rôle
// vide est enregistré comme normal (le rôle relu est alors normal) ; un rôle
// hors normal, reception_seule et offre_plus est refusé par la base.
func (s *InviteStore) AddPending(ctx context.Context, p invite.Pending) (invite.Pending, error) {
	if p.GroupID == 0 {
		return invite.Pending{}, errs.Internal(nil)
	}
	if p.Role == "" {
		p.Role = participants.RoleNormal
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO registration_pending
			(group_id, ref, first_name, name, name_key, email, email_key, shared_address, lang, role, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.GroupID, p.Ref, p.FirstName, p.Name, p.NameKey, p.Email, p.EmailKey,
		boolInt(p.SharedAddress), p.Lang, string(p.Role), toNanos(p.CreatedAt), toNanos(p.ExpiresAt))
	if err != nil {
		return invite.Pending{}, errs.Internal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return invite.Pending{}, errs.Internal(err)
	}
	p.ID = id
	return p, nil
}

// DeletePending implémente invite.Store ; une inscription d'un autre groupe
// n'est jamais supprimée.
func (s *InviteStore) DeletePending(ctx context.Context, groupID, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM registration_pending WHERE group_id = ? AND id = ?`, groupID, id); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// ListPending implémente invite.Store : inscriptions non expirées du groupe,
// dans l'ordre d'arrivée. L'instant zéro (time.Time{}) désigne toutes les
// inscriptions du groupe, expirées comprises (la purge au démarrage s'en sert),
// comme invite.MemoryStore.
func (s *InviteStore) ListPending(ctx context.Context, groupID int64, now time.Time) ([]invite.Pending, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, group_id, ref, first_name, name, name_key, email, email_key, shared_address, lang, role, created_at, expires_at
		 FROM registration_pending WHERE group_id = ? AND expires_at > ? ORDER BY id`,
		groupID, cutoff(now))
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []invite.Pending
	for rows.Next() {
		var (
			p                invite.Pending
			shared           int
			role             string
			created, expires int64
		)
		if err := rows.Scan(&p.ID, &p.GroupID, &p.Ref, &p.FirstName, &p.Name, &p.NameKey,
			&p.Email, &p.EmailKey, &shared, &p.Lang, &role, &created, &expires); err != nil {
			return nil, errs.Internal(err)
		}
		p.SharedAddress = shared == 1
		p.Role = participants.DrawRole(role)
		p.CreatedAt = fromNanos(created)
		p.ExpiresAt = fromNanos(expires)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// CountPending implémente invite.Store.
func (s *InviteStore) CountPending(ctx context.Context, groupID int64, now time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM registration_pending WHERE group_id = ? AND expires_at > ?`,
		groupID, cutoff(now)).Scan(&n); err != nil {
		return 0, errs.Internal(err)
	}
	return n, nil
}

func (s *InviteStore) exists(ctx context.Context, column string, groupID int64, key string, now time.Time) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM registration_pending WHERE group_id = ? AND `+column+` = ? AND expires_at > ? LIMIT 1`,
		groupID, key, cutoff(now)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errs.Internal(err)
	}
	return true, nil
}

// PendingNameExists implémente invite.Store (clé de participants.NormalizeName,
// comparée telle quelle).
func (s *InviteStore) PendingNameExists(ctx context.Context, groupID int64, nameKey string, now time.Time) (bool, error) {
	return s.exists(ctx, "name_key", groupID, nameKey, now)
}

// PendingEmailExists implémente invite.Store (adresse déjà en minuscules).
func (s *InviteStore) PendingEmailExists(ctx context.Context, groupID int64, emailKey string, now time.Time) (bool, error) {
	return s.exists(ctx, "email_key", groupID, emailKey, now)
}

// PurgeExpired supprime, tous groupes confondus, les inscriptions en attente
// expirées à l'instant now (expires_at <= now) ainsi que leurs codes de
// vérification, dans une seule transaction. Il renvoie le nombre
// d'inscriptions purgées. À appeler par la tâche planifiée sur l'horloge de
// F1 ; rejouer est sans effet.
func (s *InviteStore) PurgeExpired(ctx context.Context, now time.Time) (n int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errs.Internal(err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	at := toNanos(now)
	// Les codes d'abord : ils sont retrouvés par leur inscription expirée.
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM registration_codes WHERE EXISTS (
			SELECT 1 FROM registration_pending p
			WHERE p.group_id = registration_codes.group_id
			  AND p.id = registration_codes.registration_id
			  AND p.expires_at <= ?)`, at); err != nil {
		return 0, errs.Internal(err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM registration_pending WHERE expires_at <= ?`, at)
	if err != nil {
		return 0, errs.Internal(err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, errs.Internal(err)
	}
	if err = tx.Commit(); err != nil {
		return 0, errs.Internal(err)
	}
	return int(affected), nil
}

// CodeStore implémente code.Store sur une base SQLite migrée.
type CodeStore struct {
	db *sql.DB
}

var _ code.Store = (*CodeStore)(nil)

// NewCodeStore renvoie le magasin des codes de vérification ; d doit avoir
// reçu les migrations de l'inscription.
func NewCodeStore(d *sql.DB) *CodeStore { return &CodeStore{db: d} }

// Put implémente code.Store : remplace en une écriture le code de
// l'inscription (ancien code et ancien jeton oubliés).
func (s *CodeStore) Put(ctx context.Context, r code.Record) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registration_codes
			(group_id, registration_id, code_hash, token_hash, issued_at, expires_at, attempts, consumed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (group_id, registration_id) DO UPDATE SET
			code_hash = excluded.code_hash, token_hash = excluded.token_hash,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at,
			attempts = excluded.attempts, consumed = excluded.consumed`,
		r.GroupID, r.RegistrationID, r.CodeHash, r.TokenHash, toNanos(r.IssuedAt), toNanos(r.ExpiresAt),
		r.Attempts, boolInt(r.Consumed))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

const recordColumns = `group_id, registration_id, code_hash, token_hash, issued_at, expires_at, attempts, consumed`

func scanRecord(row *sql.Row) (code.Record, bool, error) {
	var (
		r               code.Record
		issued, expires int64
		consumed        int
	)
	err := row.Scan(&r.GroupID, &r.RegistrationID, &r.CodeHash, &r.TokenHash, &issued, &expires, &r.Attempts, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return code.Record{}, false, nil
	}
	if err != nil {
		return code.Record{}, false, errs.Internal(err)
	}
	r.IssuedAt = fromNanos(issued)
	r.ExpiresAt = fromNanos(expires)
	r.Consumed = consumed == 1
	return r, true, nil
}

// Get implémente code.Store.
func (s *CodeStore) Get(ctx context.Context, groupID, registrationID int64) (code.Record, bool, error) {
	return scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+recordColumns+` FROM registration_codes WHERE group_id = ? AND registration_id = ?`,
		groupID, registrationID))
}

// FindByToken implémente code.Store : dans ce groupe seulement.
func (s *CodeStore) FindByToken(ctx context.Context, groupID int64, tokenHash string) (code.Record, bool, error) {
	return scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+recordColumns+` FROM registration_codes WHERE group_id = ? AND token_hash = ?
		 ORDER BY registration_id LIMIT 1`,
		groupID, tokenHash))
}

// AddAttempt implémente code.Store : une seule instruction, conditionnée par
// l'empreinte du code courant.
func (s *CodeStore) AddAttempt(ctx context.Context, groupID, registrationID int64, codeHash string) (int, bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`UPDATE registration_codes SET attempts = attempts + 1
		 WHERE group_id = ? AND registration_id = ? AND code_hash = ?
		 RETURNING attempts`,
		groupID, registrationID, codeHash).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, errs.Internal(err)
	}
	return n, true, nil
}

// Consume implémente code.Store : une seule instruction conditionnelle,
// atomique face aux saisies simultanées.
func (s *CodeStore) Consume(ctx context.Context, groupID, registrationID int64, codeHash string, now time.Time, maxAttempts int) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE registration_codes SET consumed = 1
		 WHERE group_id = ? AND registration_id = ? AND code_hash = ?
		   AND consumed = 0 AND expires_at > ? AND attempts <= ?`,
		groupID, registrationID, codeHash, toNanos(now), maxAttempts)
	if err != nil {
		return false, errs.Internal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, errs.Internal(err)
	}
	return n == 1, nil
}

// Delete implémente code.Store (sans erreur si le code n'existe pas).
func (s *CodeStore) Delete(ctx context.Context, groupID, registrationID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM registration_codes WHERE group_id = ? AND registration_id = ?`,
		groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	return nil
}
