package verify

import (
	"context"
	"time"

	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// PendingCount implémente ports.RegistrationState : nombre d'inscriptions en
// attente non expirées et non ignorées du groupe (celles que le tirage doit
// faire traiter).
func (s *Service) PendingCount(ctx context.Context, groupID int64) (int, error) {
	ps, err := s.cfg.Pending.ListPending(ctx, groupID, s.cfg.Clock.Now().UTC())
	if err != nil {
		return 0, errs.Internal(err)
	}
	ig, err := s.cfg.Store.Ignored(ctx, groupID)
	if err != nil {
		return 0, errs.Internal(err)
	}
	n := 0
	for _, p := range ps {
		if p.GroupID == groupID && !ig[p.ID] {
			n++
		}
	}
	return n, nil
}

// Resolve implémente ports.RegistrationState. « ignorer » : les inscriptions
// restent visibles de l'administrateur mais sont fermées, sans code valable,
// et ne sont plus comptées. « supprimer » : elles sont purgées (ignorées
// comprises).
func (s *Service) Resolve(ctx context.Context, groupID int64, how ports.RegistrationResolution) error {
	if how != ports.ResolveIgnore && how != ports.ResolveDelete {
		return errs.Validation(msgResolve).WithCode(codeResolve)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Clock.Now().UTC()
	ps, err := s.cfg.Pending.ListPending(ctx, groupID, now)
	if err != nil {
		return errs.Internal(err)
	}
	n := 0
	for _, p := range ps {
		if p.GroupID != groupID {
			continue
		}
		switch how {
		case ports.ResolveIgnore:
			if err := s.cfg.Codes.Invalidate(ctx, groupID, p.ID); err != nil {
				return errs.Internal(err)
			}
			if err := s.cfg.Store.SetIgnored(ctx, groupID, p.ID, now); err != nil {
				return errs.Internal(err)
			}
		case ports.ResolveDelete:
			if err := s.drop(ctx, groupID, p.ID); err != nil {
				return err
			}
		}
		n++
	}
	s.cfg.Logger.Info("inscription : inscriptions en attente résolues", "groupe", groupID, "choix", string(how), "nombre", n)
	return nil
}

// schedulePurge planifie la purge de l'inscription à son expiration.
func (s *Service) schedulePurge(groupID, registrationID int64, at time.Time) {
	if s.cfg.Scheduler == nil {
		return
	}
	s.cfg.Scheduler.At(at, func(ctx context.Context) error {
		return s.purgeOne(ctx, groupID, registrationID)
	})
}

// purgeOne retire une inscription arrivée à expiration, son code, son état
// ignoré et sa trace de vérification.
func (s *Service) purgeOne(ctx context.Context, groupID, registrationID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.drop(ctx, groupID, registrationID); err != nil {
		return err
	}
	if err := s.cfg.Store.DeleteOutcome(ctx, groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// PurgeExpired retire toutes les inscriptions expirées du groupe (48 heures
// après leur création), avec leur code, leur état ignoré et leur trace de
// vérification, et renvoie leur nombre. Il complète la purge planifiée, par
// exemple au démarrage, quand les tâches en mémoire ont été perdues.
func (s *Service) PurgeExpired(ctx context.Context, groupID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Clock.Now().UTC()
	// Instant zéro : toutes les inscriptions du groupe, expirées comprises.
	ps, err := s.cfg.Pending.ListPending(ctx, groupID, time.Time{})
	if err != nil {
		return 0, errs.Internal(err)
	}
	n := 0
	for _, p := range ps {
		if p.GroupID != groupID || p.ExpiresAt.After(now) {
			continue
		}
		if err := s.drop(ctx, groupID, p.ID); err != nil {
			return n, err
		}
		n++
	}
	if _, err := s.cfg.Store.DeleteOutcomesExpired(ctx, groupID, now); err != nil {
		return n, errs.Internal(err)
	}
	return n, nil
}
