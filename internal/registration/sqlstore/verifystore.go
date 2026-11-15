package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"secretsanta/internal/registration/verify"
	"secretsanta/internal/shared/errs"
)

// VerifyStore implémente verify.Store sur une base SQLite migrée (migration
// 002 de l'inscription) : traces des inscriptions vérifiées, inscriptions
// ignorées et journal des actions de l'administrateur. Il respecte le contrat
// de verify.MemoryStore. Toute opération est bornée au groupe donné.
type VerifyStore struct {
	db *sql.DB
}

var _ verify.Store = (*VerifyStore)(nil)

// NewVerifyStore renvoie le magasin ; d doit avoir reçu les migrations de
// l'inscription.
func NewVerifyStore(d *sql.DB) *VerifyStore { return &VerifyStore{db: d} }

// SaveOutcome implémente verify.Store (création ou remplacement).
func (s *VerifyStore) SaveOutcome(ctx context.Context, o verify.Outcome) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registration_outcomes
			(group_id, registration_id, ref, participant_id, verified_at, by_admin, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (group_id, registration_id) DO UPDATE SET
			ref = excluded.ref, participant_id = excluded.participant_id,
			verified_at = excluded.verified_at, by_admin = excluded.by_admin,
			expires_at = excluded.expires_at`,
		o.GroupID, o.RegistrationID, o.Ref, o.ParticipantID, toNanos(o.VerifiedAt),
		boolInt(o.ByAdmin), toNanos(o.ExpiresAt))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

const outcomeColumns = `group_id, registration_id, ref, participant_id, verified_at, by_admin, expires_at`

func scanOutcome(row *sql.Row) (verify.Outcome, bool, error) {
	var (
		o                 verify.Outcome
		verified, expires int64
		byAdmin           int
	)
	err := row.Scan(&o.GroupID, &o.RegistrationID, &o.Ref, &o.ParticipantID, &verified, &byAdmin, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return verify.Outcome{}, false, nil
	}
	if err != nil {
		return verify.Outcome{}, false, errs.Internal(err)
	}
	o.VerifiedAt = fromNanos(verified)
	o.ByAdmin = byAdmin == 1
	o.ExpiresAt = fromNanos(expires)
	return o, true, nil
}

// Outcome implémente verify.Store.
func (s *VerifyStore) Outcome(ctx context.Context, groupID, registrationID int64) (verify.Outcome, bool, error) {
	return scanOutcome(s.db.QueryRowContext(ctx,
		`SELECT `+outcomeColumns+` FROM registration_outcomes WHERE group_id = ? AND registration_id = ?`,
		groupID, registrationID))
}

// OutcomeByRef implémente verify.Store : une référence vide ne correspond à
// rien.
func (s *VerifyStore) OutcomeByRef(ctx context.Context, groupID int64, ref string) (verify.Outcome, bool, error) {
	if ref == "" {
		return verify.Outcome{}, false, nil
	}
	return scanOutcome(s.db.QueryRowContext(ctx,
		`SELECT `+outcomeColumns+` FROM registration_outcomes WHERE group_id = ? AND ref = ?
		 ORDER BY registration_id LIMIT 1`,
		groupID, ref))
}

// DeleteOutcome implémente verify.Store (sans erreur si la trace n'existe pas).
func (s *VerifyStore) DeleteOutcome(ctx context.Context, groupID, registrationID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM registration_outcomes WHERE group_id = ? AND registration_id = ?`,
		groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// DeleteOutcomesExpired implémente verify.Store : retire les traces du groupe
// dont l'expiration est atteinte (expires_at <= now).
func (s *VerifyStore) DeleteOutcomesExpired(ctx context.Context, groupID int64, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM registration_outcomes WHERE group_id = ? AND expires_at <= ?`, groupID, toNanos(now))
	if err != nil {
		return 0, errs.Internal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, errs.Internal(err)
	}
	return int(n), nil
}

// SetIgnored implémente verify.Store.
func (s *VerifyStore) SetIgnored(ctx context.Context, groupID, registrationID int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registration_ignored (group_id, registration_id, ignored_at) VALUES (?, ?, ?)
		 ON CONFLICT (group_id, registration_id) DO UPDATE SET ignored_at = excluded.ignored_at`,
		groupID, registrationID, toNanos(at))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// Ignored implémente verify.Store : identifiants des inscriptions ignorées du
// groupe (une table vide donne une carte vide, jamais nil).
func (s *VerifyStore) Ignored(ctx context.Context, groupID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT registration_id FROM registration_ignored WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Internal(err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// ClearIgnored implémente verify.Store (sans erreur si rien n'est marqué).
func (s *VerifyStore) ClearIgnored(ctx context.Context, groupID, registrationID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM registration_ignored WHERE group_id = ? AND registration_id = ?`,
		groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// AppendAudit implémente verify.Store.
func (s *VerifyStore) AppendAudit(ctx context.Context, e verify.AuditEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registration_audit
			(group_id, registration_id, action, actor, at, first_name, name, participant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.GroupID, e.RegistrationID, string(e.Action), e.Actor, toNanos(e.At), e.FirstName, e.Name, e.ParticipantID)
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// Audit implémente verify.Store : le journal du groupe, du plus ancien au
// plus récent (à instant égal, dans l'ordre d'enregistrement).
func (s *VerifyStore) Audit(ctx context.Context, groupID int64) ([]verify.AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_id, registration_id, action, actor, at, first_name, name, participant_id
		 FROM registration_audit WHERE group_id = ? ORDER BY at, id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []verify.AuditEntry
	for rows.Next() {
		var (
			e      verify.AuditEntry
			action string
			at     int64
		)
		if err := rows.Scan(&e.GroupID, &e.RegistrationID, &action, &e.Actor, &at,
			&e.FirstName, &e.Name, &e.ParticipantID); err != nil {
			return nil, errs.Internal(err)
		}
		e.Action = verify.Action(action)
		e.At = fromNanos(at)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// GroupIDs renvoie, dans l'ordre croissant, les groupes qui portent encore un
// état d'inscription (inscription en attente, trace de vérification ou
// inscription ignorée) : ceux que la purge au démarrage doit parcourir. Les
// autres groupes n'ont rien à purger.
func (s *VerifyStore) GroupIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_id FROM registration_pending
		 UNION SELECT group_id FROM registration_outcomes
		 UNION SELECT group_id FROM registration_ignored
		 ORDER BY 1`)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// cutoff est la borne de « vivant » (expires_at > cutoff) pour l'instant now :
// l'instant zéro désigne toutes les inscriptions, expirées comprises, comme
// dans invite.MemoryStore.
func cutoff(now time.Time) int64 {
	if now.IsZero() {
		return math.MinInt64
	}
	return toNanos(now)
}
