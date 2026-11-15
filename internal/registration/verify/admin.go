package verify

import (
	"context"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/shared/errs"
)

// AdminValidation est la réponse de « Valider sans code ». Elle ne contient
// aucune paire, aucune cible et aucun secret (le lien personnel ne part que
// par le courriel « bienvenue »).
type AdminValidation struct {
	RegistrationID int64
	ParticipantID  int64
	FirstName      string
	Name           string
	ValidatedAt    time.Time
}

// PendingView est une inscription en attente vue par l'administrateur.
type PendingView struct {
	ID        int64
	FirstName string
	Name      string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
	// Ignored est vrai pour une inscription « ignorée » avant le tirage :
	// conservée ici, mais fermée et sans code valable.
	Ignored bool
}

func errNotFound() error { return errs.NotFound(msgNotFound).WithCode(codeNotFound) }
func errIgnored() error  { return errs.Forbidden(msgIgnored).WithCode(codeIgnored) }

// adminPending renvoie l'inscription en attente du groupe de
// l'administrateur ; une inscription d'un autre groupe est introuvable.
func (s *Service) adminPending(ctx context.Context, a participants.Actor, groupID, registrationID int64) (invite.Pending, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return invite.Pending{}, err
	}
	p, ok, err := s.livePending(ctx, groupID, s.cfg.Clock.Now().UTC(), byID(registrationID))
	if err != nil {
		return invite.Pending{}, errs.Internal(err)
	}
	if !ok {
		return invite.Pending{}, errNotFound()
	}
	return p, nil
}

func (s *Service) audit(ctx context.Context, p invite.Pending, act Action, at time.Time, participantID int64) {
	e := AuditEntry{GroupID: p.GroupID, RegistrationID: p.ID, Action: act, Actor: actorAdmin, At: at,
		FirstName: p.FirstName, Name: p.Name, ParticipantID: participantID}
	if err := s.cfg.Store.AppendAudit(ctx, e); err != nil {
		s.cfg.Logger.Error("inscription : journal non enregistré", "groupe", p.GroupID, "inscription", p.ID, "erreur", err)
	}
	s.cfg.Logger.Info("inscription : action de l'administrateur",
		"groupe", p.GroupID, "inscription", p.ID, "action", string(act), "date", at)
}

// ValidateWithoutCode fait de l'inscription un participant sans code
// (« Valider sans code ») : même chemin que la vérification (CreateVerified,
// courriel « bienvenue »), action journalisée (administrateur, date,
// inscription). Refusée pour une inscription ignorée ou après le tirage.
func (s *Service) ValidateWithoutCode(ctx context.Context, a participants.Actor, groupID, registrationID int64) (*AdminValidation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.adminPending(ctx, a, groupID, registrationID)
	if err != nil {
		return nil, err
	}
	if ig, err := s.isIgnored(ctx, groupID, p.ID); err != nil {
		return nil, errs.Internal(err)
	} else if ig {
		return nil, errIgnored()
	}
	if err := s.gate(ctx, groupID); err != nil {
		return nil, err
	}
	now := s.cfg.Clock.Now().UTC()
	res, err := s.complete(ctx, p, now, true)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, ActionValidate, now, res.ParticipantID)
	return &AdminValidation{RegistrationID: p.ID, ParticipantID: res.ParticipantID,
		FirstName: p.FirstName, Name: p.Name, ValidatedAt: now}, nil
}

// ResendCode émet un nouveau code pour l'inscription (« Renvoyer le code ») :
// l'ancien code et l'ancien lien cessent de fonctionner. Il respecte le délai
// de 60 secondes (code.ErrTooSoon) et le plafond par adresse de F11.3.
func (s *Service) ResendCode(ctx context.Context, a participants.Actor, groupID, registrationID int64) (code.Issued, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.adminPending(ctx, a, groupID, registrationID)
	if err != nil {
		return code.Issued{}, err
	}
	if ig, err := s.isIgnored(ctx, groupID, p.ID); err != nil {
		return code.Issued{}, errs.Internal(err)
	} else if ig {
		return code.Issued{}, errIgnored()
	}
	if err := s.gate(ctx, groupID); err != nil {
		return code.Issued{}, err
	}
	g, err := s.cfg.Groups.ByID(ctx, groupID)
	if err != nil {
		return code.Issued{}, err
	}
	issued, err := s.cfg.Codes.Issue(ctx, s.request(ctx, g, p))
	if err != nil {
		return code.Issued{}, err
	}
	s.audit(ctx, p, ActionResend, s.cfg.Clock.Now().UTC(), 0)
	return issued, nil
}

// DeleteRegistration retire l'inscription (« Supprimer ») et invalide son
// code et son lien de confirmation.
func (s *Service) DeleteRegistration(ctx context.Context, a participants.Actor, groupID, registrationID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.adminPending(ctx, a, groupID, registrationID)
	if err != nil {
		return err
	}
	if err := s.drop(ctx, groupID, p.ID); err != nil {
		return err
	}
	s.audit(ctx, p, ActionDelete, s.cfg.Clock.Now().UTC(), 0)
	return nil
}

// drop retire l'inscription, son code et son état ignoré. L'appelant tient
// s.mu.
func (s *Service) drop(ctx context.Context, groupID, registrationID int64) error {
	if err := s.cfg.Codes.Invalidate(ctx, groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	if err := s.cfg.Pending.DeletePending(ctx, groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	if err := s.cfg.Store.ClearIgnored(ctx, groupID, registrationID); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// ListPending renvoie les inscriptions en attente non expirées du groupe de
// l'administrateur, ignorées comprises (marquées).
func (s *Service) ListPending(ctx context.Context, a participants.Actor, groupID int64) ([]PendingView, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	ps, err := s.cfg.Pending.ListPending(ctx, groupID, s.cfg.Clock.Now().UTC())
	if err != nil {
		return nil, errs.Internal(err)
	}
	ig, err := s.cfg.Store.Ignored(ctx, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	out := make([]PendingView, 0, len(ps))
	for _, p := range ps {
		if p.GroupID != groupID {
			continue
		}
		out = append(out, PendingView{ID: p.ID, FirstName: p.FirstName, Name: p.Name, Email: p.Email,
			CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt, Ignored: ig[p.ID]})
	}
	return out, nil
}

// Audit renvoie le journal des actions de l'administrateur sur les
// inscriptions de son groupe.
func (s *Service) Audit(ctx context.Context, a participants.Actor, groupID int64) ([]AuditEntry, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	es, err := s.cfg.Store.Audit(ctx, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return es, nil
}
