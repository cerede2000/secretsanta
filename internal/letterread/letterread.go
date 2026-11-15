// Package letterread porte la lecture de la lettre au Père Noël par son
// tireur (F5b) : lecture seule de la lettre de sa propre cible, seulement
// après la révélation de CETTE cible, cachet « lue par le Père Noël » posé
// par lettre et par version, et archivage de la lettre de l'ancienne cible
// lors d'un ajustement du tirage.
//
// Règles :
//   - Le tireur ne peut jamais répondre : ce paquet n'expose aucune écriture
//     à son intention (ni réponse, ni question, ni commentaire) et ses routes
//     sont toutes en lecture (GET).
//   - Le cachet vu par le tiré ne porte ni date ni identité : un simple
//     booléen. L'administrateur ne voit que « lettre lue », par tiré.
//   - L'archive d'une lettre n'est jamais servie à un tireur : le nouveau
//     tireur reçoit la lettre existante (version courante), sans le cachet de
//     l'ancien tireur.
package letterread

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Module est le nom du module dans schema_migrations. Ses migrations
// s'appliquent après celles de letter et de draw.
const Module = "letterread"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par
// l'orchestrateur après celles de letter et de draw.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Messages français.
const (
	msgAuth        = "Authentification requise."
	msgForbidden   = "Vous n'avez pas le droit d'effectuer cette action."
	msgUnavailable = "Cette lettre n'est pas disponible."
)

// Targets donne les cibles de la personne connectée, derrière la porte de
// révélation (satisfait par *draw.Service).
type Targets interface {
	MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*draw.MyTargets, error)
}

// Letters lit les lettres (satisfait par *letter.Service).
type Letters interface {
	Get(ctx context.Context, groupID, participantID int64) (*letter.Letter, error)
	ImageFile(ctx context.Context, groupID, ideaID int64) ([]byte, string, error)
}

// Archiver est le port d'archivage consommé par l'ajustement du tirage
// (F4c) : dans la transaction de l'ajustement, il archive la lettre des
// tirés dont le tireur change (satisfait par *Service).
type Archiver interface {
	ArchiveTx(ctx context.Context, tx *sql.Tx, groupID int64, receiverIDs []int64) error
}

var _ Archiver = (*Service)(nil)

// Stamp est le cachet vu par le tiré : lu ou non, rien d'autre (ni date, ni
// identité).
type Stamp struct {
	Read bool
}

// Service sert la lecture de la lettre par son tireur.
type Service struct {
	db      *sql.DB
	clock   clock.Clock
	targets Targets
	letters Letters
}

// NewService construit le service. targets doit appliquer la porte de
// révélation (draw.Service construit avec la porte de reveal).
func NewService(d *sql.DB, c clock.Clock, targets Targets, letters Letters) *Service {
	return &Service{db: d, clock: c, targets: targets, letters: letters}
}

func requireParticipant(a participants.Actor, groupID int64) error {
	switch {
	case a.Kind == participants.ActorAnonymous:
		return errs.New(errs.KindUnauthorized, msgAuth)
	case a.Kind != participants.ActorParticipant || a.GroupID != groupID || a.ParticipantID <= 0:
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

func requireAdmin(a participants.Actor, groupID int64) error {
	switch {
	case a.Kind == participants.ActorAnonymous:
		return errs.New(errs.KindUnauthorized, msgAuth)
	case a.Kind != participants.ActorAdmin || a.GroupID != groupID:
		return errs.Forbidden(msgForbidden)
	}
	return nil
}

func unavailable() error { return errs.NotFound(msgUnavailable) }

// target vérifie que pairID est une cible de la personne connectée, que la
// révélation est ouverte et que cette cible-là a été découverte ; il renvoie
// le tiré. Tout refus donne la même erreur.
func (s *Service) target(ctx context.Context, a participants.Actor, groupID, pairID int64) (int64, error) {
	if err := requireParticipant(a, groupID); err != nil {
		return 0, err
	}
	mine, err := s.targets.MyTargets(ctx, a, groupID)
	if err != nil {
		var te *errs.Error
		if errors.As(err, &te) {
			return 0, err
		}
		return 0, errs.Internal(err)
	}
	if mine == nil || !mine.Done || !mine.Ready {
		return 0, unavailable()
	}
	for _, t := range mine.Targets {
		if t.PairID == pairID {
			if !t.Discovered {
				return 0, unavailable()
			}
			return t.ReceiverID, nil
		}
	}
	return 0, unavailable()
}

// stillCurrent revérifie, après lecture, que la paire est toujours celle du
// tirage actuel entre ce tireur et ce tiré (un ajustement concurrent).
func (s *Service) stillCurrent(ctx context.Context, groupID, pairID, giverID, receiverID int64) (bool, error) {
	var ok int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE p.id = ? AND p.giver_id = ? AND p.receiver_id = ? AND d.group_id = ? AND d.status = 'actuel'
		AND p.discovered_at IS NOT NULL)`, pairID, giverID, receiverID, groupID).Scan(&ok)
	if err != nil {
		return false, errs.Internal(err)
	}
	return ok == 1, nil
}

// Read renvoie, en lecture seule, la lettre de la cible pairID de la personne
// connectée, seulement après la révélation et la découverte de cette cible.
// La première ouverture d'une version pose le cachet de cette lettre (et
// d'elle seule). Une lettre jamais écrite est renvoyée vide (version 0),
// sans cachet.
func (s *Service) Read(ctx context.Context, a participants.Actor, groupID, pairID int64) (*letter.Letter, error) {
	receiver, err := s.target(ctx, a, groupID, pairID)
	if err != nil {
		return nil, err
	}
	l, err := s.letters.Get(ctx, groupID, receiver)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return nil, unavailable()
		}
		return nil, err
	}
	ok, err := s.stillCurrent(ctx, groupID, pairID, a.ParticipantID, receiver)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, unavailable()
	}
	if l.Version > 0 {
		// Le cachet n'est posé que si la paire existe encore et que la
		// version lue est toujours celle de la lettre.
		if _, err := s.db.ExecContext(ctx, `INSERT INTO letterread_stamps (letter_id, pair_id, version)
			SELECT l.id, ?, l.version FROM letters l
			WHERE l.id = ? AND l.version = ? AND l.participant_id = ? AND l.group_id = ?
			AND EXISTS (SELECT 1 FROM draw_pairs p WHERE p.id = ? AND p.receiver_id = l.participant_id)
			ON CONFLICT(letter_id) DO UPDATE SET pair_id = excluded.pair_id, version = excluded.version`,
			pairID, l.ID, l.Version, receiver, groupID, pairID); err != nil {
			return nil, errs.Internal(err)
		}
	}
	return l, nil
}

// Image renvoie l'image d'une idée de la lettre de la cible pairID, aux
// mêmes conditions que Read (sans poser de cachet).
func (s *Service) Image(ctx context.Context, a participants.Actor, groupID, pairID, ideaID int64) ([]byte, string, error) {
	receiver, err := s.target(ctx, a, groupID, pairID)
	if err != nil {
		return nil, "", err
	}
	var ok int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM letter_ideas i JOIN letters l ON l.id = i.letter_id
		WHERE i.id = ? AND l.participant_id = ? AND l.group_id = ?)`, ideaID, receiver, groupID).Scan(&ok); err != nil {
		return nil, "", errs.Internal(err)
	}
	if ok != 1 {
		return nil, "", unavailable()
	}
	still, err := s.stillCurrent(ctx, groupID, pairID, a.ParticipantID, receiver)
	if err != nil {
		return nil, "", err
	}
	if !still {
		return nil, "", unavailable()
	}
	b, mime, err := s.letters.ImageFile(ctx, groupID, ideaID)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return nil, "", unavailable()
		}
		return nil, "", err
	}
	return b, mime, nil
}

// stampedClause est la condition SQL « la version actuelle de la lettre l a
// été lue par le tireur actuel de son auteur ».
const stampedClause = `EXISTS (SELECT 1 FROM letterread_stamps s
	JOIN draw_pairs p ON p.id = s.pair_id JOIN draws d ON d.id = p.draw_id
	WHERE s.letter_id = l.id AND s.version = l.version AND p.receiver_id = l.participant_id
	AND d.group_id = l.group_id AND d.status = 'actuel')`

// Stamp renvoie le cachet de la lettre de la personne connectée (le tiré) :
// lue ou non dans sa version actuelle, sans date ni identité.
func (s *Service) Stamp(ctx context.Context, a participants.Actor, groupID int64) (Stamp, error) {
	if err := requireParticipant(a, groupID); err != nil {
		return Stamp{}, err
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM letters l
		WHERE l.group_id = ? AND l.participant_id = ? AND l.version > 0 AND `+stampedClause+`)`,
		groupID, a.ParticipantID).Scan(&n)
	if err != nil {
		return Stamp{}, errs.Internal(err)
	}
	return Stamp{Read: n == 1}, nil
}

// AdminRead renvoie à l'administrateur, pour chaque tiré dont la lettre
// actuelle a été lue, « lettre lue » (vrai). Aucun tireur, aucune paire.
func (s *Service) AdminRead(ctx context.Context, a participants.Actor, groupID int64) (map[int64]bool, error) {
	if err := requireAdmin(a, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT l.participant_id FROM letters l
		WHERE l.group_id = ? AND l.version > 0 AND `+stampedClause, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
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

// snapshot est l'instantané archivé d'une lettre. Il ne contient jamais le
// tireur.
type snapshot struct {
	Version int             `json:"version"`
	Intro   string          `json:"intro"`
	Ideas   []snapshotIdeas `json:"ideas"`
}

type snapshotIdeas struct {
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
}

// ArchiveTx archive, dans la transaction de l'ajustement, la lettre de
// chacun des tirés receiverIDs (ceux dont le tireur change) : un instantané
// de la version actuelle est conservé dans letter_archives et le cachet est
// retiré. L'ancien tireur ne peut plus la lire (ce n'est plus sa cible) ;
// l'archive n'est jamais servie à un tireur, le nouveau tireur reçoit la
// lettre existante sans cachet. Un tiré sans lettre écrite est ignoré.
func (s *Service) ArchiveTx(ctx context.Context, tx *sql.Tx, groupID int64, receiverIDs []int64) error {
	at := s.clock.Now().UTC().Format(time.RFC3339Nano)
	seen := map[int64]bool{}
	for _, rid := range receiverIDs {
		if seen[rid] {
			continue
		}
		seen[rid] = true
		if err := archiveOne(ctx, tx, groupID, rid, at); err != nil {
			return err
		}
	}
	return nil
}

func archiveOne(ctx context.Context, tx *sql.Tx, groupID, receiverID int64, at string) error {
	var lid int64
	var snap snapshot
	err := tx.QueryRowContext(ctx, `SELECT id, version, intro FROM letters WHERE group_id = ? AND participant_id = ?`,
		groupID, receiverID).Scan(&lid, &snap.Version, &snap.Intro)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM letterread_stamps WHERE letter_id = ?`, lid); err != nil {
		return errs.Internal(err)
	}
	if snap.Version == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT title, url, body, image_file FROM letter_ideas WHERE letter_id = ? ORDER BY position, id`, lid)
	if err != nil {
		return errs.Internal(err)
	}
	for rows.Next() {
		var i snapshotIdeas
		if err := rows.Scan(&i.Title, &i.URL, &i.Text, &i.Image); err != nil {
			rows.Close()
			return errs.Internal(err)
		}
		snap.Ideas = append(snap.Ideas, i)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errs.Internal(err)
	}
	rows.Close()
	raw, err := json.Marshal(snap)
	if err != nil {
		return errs.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO letter_archives (letter_id, version, snapshot, archived_at) VALUES (?, ?, ?, ?)`,
		lid, snap.Version, string(raw), at); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// ChangedReceivers renvoie les tirés dont le tireur change entre deux
// ensembles de paires (avant et après un ajustement), y compris ceux qui
// disparaissent : ce sont les lettres à archiver. Aucune paire n'est
// renvoyée, seulement des tirés, dans l'ordre de before.
func ChangedReceivers(before, after []draw.Pair) []int64 {
	now := make(map[int64]int64, len(after))
	for _, p := range after {
		now[p.Receiver] = p.Giver
	}
	var out []int64
	seen := map[int64]bool{}
	for _, p := range before {
		if g, ok := now[p.Receiver]; ok && g == p.Giver {
			continue
		}
		if !seen[p.Receiver] {
			seen[p.Receiver] = true
			out = append(out, p.Receiver)
		}
	}
	return out
}
