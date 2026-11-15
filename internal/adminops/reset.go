package adminops

import (
	"context"
	"crypto/sha256"
	"strconv"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/random"
)

// secretDigest est la condensation du secret d'administration de F2
// (groups : SHA-256 du secret, 32 octets) ; VerifyAdminSecret compare ce
// condensé.
func secretDigest(secret string) []byte {
	d := sha256.Sum256([]byte(secret))
	return d[:]
}

// ResetAdminSecret remplace le secret d'administration du groupe par un
// nouveau secret aléatoire (128 bits), renvoyé une seule fois : seul son
// condensé est stocké. L'ancien secret cesse aussitôt de fonctionner et
// toutes les sessions administrateur du groupe sont fermées, dans la même
// transaction.
func (s *Service) ResetAdminSecret(ctx context.Context, groupID int64) (string, error) {
	secret, err := random.Token()
	if err != nil {
		return "", errs.Internal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE groups SET admin_secret_hash = ? WHERE id = ?`, secretDigest(secret), groupID)
	if err != nil {
		return "", errs.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", errs.NotFound("Ce groupe n'existe pas.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM websec_sessions WHERE kind = 'admin' AND group_id = ?`,
		strconv.FormatInt(groupID, 10)); err != nil {
		return "", errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		return "", errs.Internal(err)
	}
	return secret, nil
}
