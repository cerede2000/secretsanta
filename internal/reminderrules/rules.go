// Package reminderrules porte les règles des relances (F7.1) : réglages par
// groupe avec bornes, fenêtre d'envoi de 9 h à 20 h dans le fuseau de
// l'application (changement d'heure compris) et garde-fou « un courriel de
// relance par personne et par jour ». Il ne planifie ni n'envoie rien.
package reminderrules

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Module est le nom du module dans schema_migrations (après groups).
const Module = "reminderrules"

// Bornes et valeurs par défaut des réglages.
const (
	DefaultFirstDelayDays = 3
	DefaultSpacingDays    = 4 // 3 puis 7 jours
	DefaultMaxPerType     = 3
	MinDelayDays          = 1
	MaxDelayDays          = 30
	MinMaxPerType         = 1
	MaxMaxPerType         = 5

	// WindowStartHour et WindowEndHour : heures locales autorisées [9 h, 20 h[.
	WindowStartHour = 9
	WindowEndHour   = 20
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées.
func Migrations() ([]db.Migration, error) { return db.LoadFS(migrationsFS, "migrations") }

// Settings sont les réglages de relance automatique d'un groupe.
type Settings struct {
	Enabled        bool
	FirstDelayDays int // délai de la première relance après le fait déclencheur
	SpacingDays    int // écart entre deux relances successives
	MaxPerType     int // plafond de relances par type et par personne
}

// DefaultSettings : actif, 3 puis 7 jours, plafond 3.
func DefaultSettings() Settings {
	return Settings{Enabled: true, FirstDelayDays: DefaultFirstDelayDays, SpacingDays: DefaultSpacingDays, MaxPerType: DefaultMaxPerType}
}

// Validate refuse les valeurs hors bornes.
func (s Settings) Validate() error {
	if s.FirstDelayDays < MinDelayDays || s.FirstDelayDays > MaxDelayDays {
		return errs.Validation("Le délai de la première relance doit être de 1 à 30 jours.")
	}
	if s.SpacingDays < MinDelayDays || s.SpacingDays > MaxDelayDays {
		return errs.Validation("L'espacement entre deux relances doit être de 1 à 30 jours.")
	}
	if s.MaxPerType < MinMaxPerType || s.MaxPerType > MaxMaxPerType {
		return errs.Validation("Le plafond de relances doit être de 1 à 5.")
	}
	return nil
}

// Offset donne le délai après le fait déclencheur de la relance n (0 = première).
// Il renvoie false si n atteint le plafond.
func (s Settings) Offset(n int) (time.Duration, bool) {
	if n < 0 || n >= s.MaxPerType {
		return 0, false
	}
	return time.Duration(s.FirstDelayDays+n*s.SpacingDays) * 24 * time.Hour, true
}

// Service porte les règles ; le fuseau vient de la configuration (APP_TIMEZONE).
type Service struct {
	db  *sql.DB
	clk clock.Clock
	loc *time.Location
}

// NewService crée le service. Un fuseau nil vaut UTC.
func NewService(d *sql.DB, c clock.Clock, loc *time.Location) *Service {
	if loc == nil {
		loc = time.UTC
	}
	return &Service{db: d, clk: c, loc: loc}
}

// Location renvoie le fuseau de la fenêtre d'envoi.
func (s *Service) Location() *time.Location { return s.loc }

// Settings lit les réglages du groupe (défauts si aucune ligne).
func (s *Service) Settings(ctx context.Context, groupID int64) (Settings, error) {
	var st Settings
	var en int
	err := s.db.QueryRowContext(ctx, `SELECT enabled, first_delay_days, spacing_days, max_per_type
		FROM reminder_group_rules WHERE group_id = ?`, groupID).Scan(&en, &st.FirstDelayDays, &st.SpacingDays, &st.MaxPerType)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, errs.Internal(err)
	}
	st.Enabled = en == 1
	return st, nil
}

// SetSettings valide puis enregistre les réglages du groupe.
func (s *Service) SetSettings(ctx context.Context, groupID int64, st Settings) error {
	if err := st.Validate(); err != nil {
		return err
	}
	en := 0
	if st.Enabled {
		en = 1
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM groups WHERE id = ?`, groupID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errs.NotFound("Groupe introuvable.")
		}
		return errs.Internal(err)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reminder_group_rules
		(group_id, enabled, first_delay_days, spacing_days, max_per_type) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(group_id) DO UPDATE SET enabled = excluded.enabled,
		first_delay_days = excluded.first_delay_days, spacing_days = excluded.spacing_days,
		max_per_type = excluded.max_per_type`, groupID, en, st.FirstDelayDays, st.SpacingDays, st.MaxPerType)
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// InWindow dit si t tombe dans la fenêtre d'envoi (9 h inclus, 20 h exclu, heure locale).
func (s *Service) InWindow(t time.Time) bool {
	h := t.In(s.loc).Hour()
	return h >= WindowStartHour && h < WindowEndHour
}

// NextSend renvoie t s'il est dans la fenêtre, sinon le début de la prochaine
// fenêtre (9 h locales). Le calcul passe par la date locale : un changement
// d'heure ne décale pas le début de fenêtre.
func (s *Service) NextSend(t time.Time) time.Time {
	if s.InWindow(t) {
		return t
	}
	l := t.In(s.loc)
	y, m, d := l.Date()
	if l.Hour() >= WindowEndHour {
		d++
	}
	return time.Date(y, m, d, WindowStartHour, 0, 0, 0, s.loc)
}

// DueAt renvoie l'instant d'envoi de la relance n (0 = première) pour un fait
// déclencheur à trigger, décalé dans la fenêtre ; false au-delà du plafond.
// Les délais comptent en jours civils locaux, pas en blocs de 24 h.
func (s *Service) DueAt(st Settings, trigger time.Time, n int) (time.Time, bool) {
	if n < 0 || n >= st.MaxPerType {
		return time.Time{}, false
	}
	l := trigger.In(s.loc)
	y, m, d := l.Date()
	days := st.FirstDelayDays + n*st.SpacingDays
	due := time.Date(y, m, d+days, l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), s.loc)
	return s.NextSend(due), true
}

// Day renvoie le jour local de t (AAAA-MM-JJ), clé du garde-fou quotidien.
func (s *Service) Day(t time.Time) string { return t.In(s.loc).Format("2006-01-02") }

// SentToday dit si la personne a déjà un courriel de relance compté aujourd'hui.
func (s *Service) SentToday(ctx context.Context, groupID, participantID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminder_daily_sends
		WHERE group_id = ? AND participant_id = ? AND sent_on = ?`,
		groupID, participantID, s.Day(s.clk.Now())).Scan(&n)
	if err != nil {
		return false, errs.Internal(err)
	}
	return n > 0, nil
}

// Claim réserve atomiquement le courriel de relance du jour pour la personne.
// Il renvoie false si un courriel (de n'importe quel type) a déjà été réservé
// aujourd'hui : l'appelant ne doit alors rien mettre en file.
func (s *Service) Claim(ctx context.Context, groupID, participantID int64, kind string) (bool, error) {
	now := s.clk.Now()
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_daily_sends
		(group_id, participant_id, sent_on, kind, claimed_at) VALUES (?, ?, ?, ?, ?)`,
		groupID, participantID, s.Day(now), kind, now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, errs.Internal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, errs.Internal(err)
	}
	return n == 1, nil
}

// Release annule la réservation du jour (échec de mise en file avant tout envoi).
func (s *Service) Release(ctx context.Context, groupID, participantID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reminder_daily_sends
		WHERE group_id = ? AND participant_id = ? AND sent_on = ?`,
		groupID, participantID, s.Day(s.clk.Now()))
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}
