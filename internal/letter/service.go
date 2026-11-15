package letter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/random"
	"secretsanta/internal/shared/sanitize"
)

const (
	msgNoParticipant = "Cette personne n'existe pas dans ce groupe."
	msgNoIdea        = "Cette idée n'existe pas."
	msgNoImage       = "Cette image n'existe pas."
)

// Service écrit la lettre. L'appelant a déjà authentifié le participant
// (session) et passe son groupe et son identifiant ; la lecture par le tireur
// relève de F5b.
type Service struct {
	db       *sql.DB
	clock    clock.Clock
	bus      ports.EventBus
	imageDir string
	log      *slog.Logger
}

// NewService construit le service ; imageDir reçoit les images téléversées.
func NewService(db *sql.DB, c clock.Clock, bus ports.EventBus, imageDir string) *Service {
	return &Service{db: db, clock: c, bus: bus, imageDir: imageDir, log: slog.Default()}
}

// Get renvoie la lettre du participant (vide, version 0, si jamais écrite).
func (s *Service) Get(ctx context.Context, groupID, participantID int64) (*Letter, error) {
	var l Letter
	var upd string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, version, intro, updated_at FROM letters WHERE group_id = ? AND participant_id = ?`,
		groupID, participantID).Scan(&l.ID, &l.Version, &l.Intro, &upd)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.checkParticipant(ctx, s.db, groupID, participantID); err != nil {
			return nil, err
		}
		return &Letter{}, nil
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	l.UpdatedAt, _ = time.Parse(time.RFC3339Nano, upd)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, url, body, image_file <> '' FROM letter_ideas WHERE letter_id = ? ORDER BY position, id`, l.ID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var i Idea
		if err := rows.Scan(&i.ID, &i.Title, &i.URL, &i.Text, &i.HasImage); err != nil {
			return nil, errs.Internal(err)
		}
		l.Ideas = append(l.Ideas, i)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return &l, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func (s *Service) checkParticipant(ctx context.Context, q queryer, groupID, participantID int64) error {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM participants WHERE id = ? AND group_id = ?`, participantID, groupID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.NotFound(msgNoParticipant)
	}
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// SetIntro remplace le texte d'introduction.
func (s *Service) SetIntro(ctx context.Context, groupID, participantID int64, intro string) (*Letter, error) {
	v, err := sanitize.Text(intro, IntroMax, "Le texte d'introduction")
	if err != nil {
		return nil, err
	}
	err = s.modify(ctx, groupID, participantID, func(tx *sql.Tx, lid int64, _ int) ([]int64, error) {
		_, err := tx.ExecContext(ctx, `UPDATE letters SET intro = ? WHERE id = ?`, v, lid)
		return nil, err
	}, nil)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, groupID, participantID)
}

// AddIdea ajoute une idée (6e refusée) et renvoie son identifiant.
func (s *Service) AddIdea(ctx context.Context, groupID, participantID int64, in IdeaInput) (int64, error) {
	c, err := cleanInput(in)
	if err != nil {
		return 0, err
	}
	_, mime, file, err := s.prepareImage(groupID, in.Image)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.modify(ctx, groupID, participantID, func(tx *sql.Tx, lid int64, ver int) ([]int64, error) {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM letter_ideas WHERE letter_id = ?`, lid).Scan(&n); err != nil {
			return nil, err
		}
		if n >= MaxIdeas {
			return nil, errs.Validation(msgMaxIdeas)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO letter_ideas (letter_id, position, title, url, image_file, image_type, body, updated_version)
			 VALUES (?, (SELECT coalesce(max(position), 0) + 1 FROM letter_ideas WHERE letter_id = ?), ?, ?, ?, ?, ?, ?)`,
			lid, lid, c.title, c.url, file, mime, c.text, ver)
		if err != nil {
			return nil, err
		}
		id, _ = res.LastInsertId()
		return []int64{id}, nil
	}, func() {
		if file != "" {
			_ = os.Remove(s.imagePath(groupID, file))
		}
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateIdea modifie une idée. Sans nouvelle image ni RemoveImage, l'image est conservée.
func (s *Service) UpdateIdea(ctx context.Context, groupID, participantID, ideaID int64, in IdeaInput) error {
	c, err := cleanInput(in)
	if err != nil {
		return err
	}
	_, mime, file, err := s.prepareImage(groupID, in.Image)
	if err != nil {
		return err
	}
	var oldFile string
	err = s.modify(ctx, groupID, participantID, func(tx *sql.Tx, lid int64, ver int) ([]int64, error) {
		var curFile, curType string
		err := tx.QueryRowContext(ctx, `SELECT image_file, image_type FROM letter_ideas WHERE id = ? AND letter_id = ?`, ideaID, lid).Scan(&curFile, &curType)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NotFound(msgNoIdea)
		}
		if err != nil {
			return nil, err
		}
		newFile, newType := curFile, curType
		switch {
		case file != "":
			newFile, newType, oldFile = file, mime, curFile
		case in.RemoveImage:
			newFile, newType, oldFile = "", "", curFile
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE letter_ideas SET title = ?, url = ?, body = ?, image_file = ?, image_type = ?, updated_version = ? WHERE id = ?`,
			c.title, c.url, c.text, newFile, newType, ver, ideaID)
		return []int64{ideaID}, err
	}, func() {
		if file != "" {
			_ = os.Remove(s.imagePath(groupID, file))
		}
	})
	if err != nil {
		return err
	}
	if oldFile != "" {
		_ = os.Remove(s.imagePath(groupID, oldFile))
	}
	return nil
}

// DeleteIdea supprime une idée (et son image).
func (s *Service) DeleteIdea(ctx context.Context, groupID, participantID, ideaID int64) error {
	var oldFile string
	err := s.modify(ctx, groupID, participantID, func(tx *sql.Tx, lid int64, _ int) ([]int64, error) {
		err := tx.QueryRowContext(ctx, `SELECT image_file FROM letter_ideas WHERE id = ? AND letter_id = ?`, ideaID, lid).Scan(&oldFile)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NotFound(msgNoIdea)
		}
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM letter_ideas WHERE id = ?`, ideaID)
		return nil, err
	}, nil)
	if err != nil {
		return err
	}
	if oldFile != "" {
		_ = os.Remove(s.imagePath(groupID, oldFile))
	}
	return nil
}

// ImageFile renvoie le contenu et le type MIME de l'image locale d'une idée du
// groupe. Aucune image distante n'est jamais chargée ni référencée.
// L'autorisation de lecture (tiré ou tireur) est vérifiée par l'appelant.
func (s *Service) ImageFile(ctx context.Context, groupID, ideaID int64) ([]byte, string, error) {
	var file, mime string
	err := s.db.QueryRowContext(ctx,
		`SELECT i.image_file, i.image_type FROM letter_ideas i JOIN letters l ON l.id = i.letter_id
		 WHERE i.id = ? AND l.group_id = ?`, ideaID, groupID).Scan(&file, &mime)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && file == "") {
		return nil, "", errs.NotFound(msgNoImage)
	}
	if err != nil {
		return nil, "", errs.Internal(err)
	}
	b, err := os.ReadFile(s.imagePath(groupID, file))
	if err != nil {
		return nil, "", errs.NotFound(msgNoImage)
	}
	return b, mime, nil
}

// MarkNotified remet à zéro l'ensemble cumulé des idées modifiées : à appeler
// quand le message regroupé a été remis à F6.
func (s *Service) MarkNotified(ctx context.Context, letterID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE letters SET pending_ideas = '' WHERE id = ?`, letterID)
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

// NotificationFor construit le modèle de notification pour le tireur à partir
// d'un événement : seul le prénom du tiré concerné y figure.
func (s *Service) NotificationFor(ctx context.Context, e ports.Event) (Notification, error) {
	rid, err := strconv.ParseInt(e.Payload[PayloadRecipientID], 10, 64)
	if err != nil {
		return Notification{}, errs.Validation("Événement de lettre invalide.")
	}
	var n Notification
	if err := s.db.QueryRowContext(ctx, `SELECT first_name FROM participants WHERE id = ? AND group_id = ?`, rid, e.GroupID).Scan(&n.RecipientFirstName); err != nil {
		return n, errs.NotFound(msgNoParticipant)
	}
	n.Version, _ = strconv.Atoi(e.Payload[PayloadVersion])
	if ids := e.Payload[PayloadIdeas]; ids != "" {
		n.IdeaCount = len(strings.Split(ids, ","))
	}
	return n, nil
}

func (s *Service) imagePath(groupID int64, file string) string {
	return filepath.Join(s.imageDir, strconv.FormatInt(groupID, 10), filepath.Base(file))
}

// prepareImage contrôle puis écrit l'image sur disque ; "" si pas d'image.
func (s *Service) prepareImage(groupID int64, data []byte) ([]byte, string, string, error) {
	if len(data) == 0 {
		return nil, "", "", nil
	}
	out, mime, err := checkImage(data)
	if err != nil {
		return nil, "", "", err
	}
	tok, err := random.Token()
	if err != nil {
		return nil, "", "", errs.Internal(err)
	}
	ext := ".png"
	if mime == "image/jpeg" {
		ext = ".jpg"
	}
	name := tok + ext
	dir := filepath.Join(s.imageDir, strconv.FormatInt(groupID, 10))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, "", "", errs.Internal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), out, 0o640); err != nil {
		return nil, "", "", errs.Internal(err)
	}
	return out, mime, name, nil
}

// modify exécute une modification dans une transaction : crée la lettre au
// besoin, incrémente la version, cumule les idées touchées, puis émet
// l'événement après validation. cleanup est appelé si la transaction échoue.
func (s *Service) modify(ctx context.Context, groupID, participantID int64,
	fn func(tx *sql.Tx, letterID int64, version int) ([]int64, error), cleanup func()) (err error) {
	fail := func(e error) error {
		if cleanup != nil {
			cleanup()
		}
		var te *errs.Error
		if errors.As(e, &te) {
			return e
		}
		return errs.Internal(e)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.checkParticipant(ctx, tx, groupID, participantID); err != nil {
		return fail(err)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339Nano)
	// Première écriture : prend le verrou avant toute lecture dépendante.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO letters (group_id, participant_id, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(participant_id) DO UPDATE SET version = version + 1, updated_at = excluded.updated_at`,
		groupID, participantID, ts, ts); err != nil {
		return fail(err)
	}
	var lid int64
	var ver int
	var pending string
	if err := tx.QueryRowContext(ctx, `SELECT id, version, pending_ideas FROM letters WHERE participant_id = ?`, participantID).Scan(&lid, &ver, &pending); err != nil {
		return fail(err)
	}
	if ver == 0 { // lettre créée à l'instant : première version
		ver = 1
		if _, err := tx.ExecContext(ctx, `UPDATE letters SET version = 1 WHERE id = ?`, lid); err != nil {
			return fail(err)
		}
	}
	changed, err := fn(tx, lid, ver)
	if err != nil {
		if strings.Contains(err.Error(), "letter_ideas_max") {
			err = errs.Validation(msgMaxIdeas)
		}
		return fail(err)
	}
	set := map[int64]bool{}
	var order []int64
	for _, p := range strings.Split(pending, ",") {
		if id, e := strconv.ParseInt(p, 10, 64); e == nil && !set[id] {
			set[id] = true
			order = append(order, id)
		}
	}
	var mine []string
	for _, id := range changed {
		mine = append(mine, strconv.FormatInt(id, 10))
		if !set[id] {
			set[id] = true
			order = append(order, id)
		}
	}
	cum := make([]string, len(order))
	for i, id := range order {
		cum[i] = strconv.FormatInt(id, 10)
	}
	cumStr := strings.Join(cum, ",")
	if _, err := tx.ExecContext(ctx, `UPDATE letters SET pending_ideas = ? WHERE id = ?`, cumStr, lid); err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO letter_versions (letter_id, version, changed_at, changed_ideas) VALUES (?, ?, ?, ?)`,
		lid, ver, ts, strings.Join(mine, ",")); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	ev := ports.Event{Name: EventUpdated, GroupID: groupID, Payload: map[string]string{
		PayloadKey:         "letter:" + strconv.FormatInt(lid, 10),
		PayloadLetterID:    strconv.FormatInt(lid, 10),
		PayloadRecipientID: strconv.FormatInt(participantID, 10),
		PayloadVersion:     strconv.Itoa(ver),
		PayloadAt:          now.Format(time.RFC3339),
		PayloadIdeas:       cumStr,
	}}
	if s.bus != nil {
		if err := s.bus.Publish(ctx, ev); err != nil {
			s.log.Warn("événement de lettre non traité", "erreur", fmt.Sprint(err))
		}
	}
	return nil
}
