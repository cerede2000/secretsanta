// Package givernotify prévient le tireur d'une lettre nouvelle ou modifiée
// (F7b) : un courriel « nouvelle idée » regroupé par tireur (et non par
// lettre), envoyé seulement quand le tireur peut déjà lire la lettre, puis
// une relance unique tant qu'une lettre nouvelle ou modifiée reste non
// ouverte. Aucun courriel ne nomme une cible ni le tireur d'un autre : le
// destinataire est seul nommé, et une personne reception_seule n'est jamais
// notifiée (elle n'a aucune lettre à lire).
package givernotify

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/letter"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reminders"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// kindReminderLog est le type du journal des relances (table reminders_sent
// de F7) pour la relance du tireur.
const kindReminderLog = "lecture"

// Gate est la porte de révélation (*reveal.Gate).
type Gate interface {
	IsOpen(ctx context.Context, groupID int64) (bool, error)
}

// Letters remet à zéro les idées cumulées (*letter.Service).
type Letters interface {
	MarkNotified(ctx context.Context, letterID int64) error
}

// Options rassemble les dépendances du service.
type Options struct {
	DB        *sql.DB
	Clock     clock.Clock
	Rules     *reminderrules.Service
	Directory reminders.Directory
	Queue     reminders.Queue
	Unsub     reminders.Unsubscriber
	Suppress  ports.SuppressionList
	Catalog   *catalog.Catalog
	Gate      Gate
	Letters   Letters // facultatif
	// LinkFor donne le lien mis dans le courriel ; par défaut la page de
	// connexion du groupe sous PublicURL.
	LinkFor   func(g reminders.GroupInfo, p participants.Participant) string
	PublicURL string
	Logger    *slog.Logger
}

// Service notifie et relance les tireurs.
type Service struct{ o Options }

// New crée le service.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.LinkFor == nil {
		base := strings.TrimRight(o.PublicURL, "/")
		o.LinkFor = func(g reminders.GroupInfo, _ participants.Participant) string {
			return base + "/" + g.Slug + "/connexion"
		}
	}
	return &Service{o: o}
}

// Subscribe abonne le service à l'événement de lettre nouvelle ou modifiée.
func (s *Service) Subscribe(bus ports.EventBus) {
	bus.Subscribe(letter.EventUpdated, s.HandleUpdated)
}

// CoalesceKey est la clé de regroupement : celle du TIREUR, pas de la lettre.
func CoalesceKey(groupID, giverID int64) string {
	return "giver:" + strconv.FormatInt(groupID, 10) + ":" + strconv.FormatInt(giverID, 10)
}

// giverOf renvoie le tireur actuel du tiré et son nombre de cibles.
func (s *Service) giverOf(ctx context.Context, groupID, receiverID int64) (giver int64, targets int, err error) {
	err = s.o.DB.QueryRowContext(ctx, `SELECT p.giver_id FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' AND p.receiver_id = ?`, groupID, receiverID).Scan(&giver)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, errs.Internal(err)
	}
	err = s.o.DB.QueryRowContext(ctx, `SELECT count(*) FROM draw_pairs p JOIN draws d ON d.id = p.draw_id
		WHERE d.group_id = ? AND d.status = 'actuel' AND p.giver_id = ?`, groupID, giver).Scan(&targets)
	if err != nil {
		return 0, 0, errs.Internal(err)
	}
	return giver, targets, nil
}

func (s *Service) person(ctx context.Context, groupID, id int64) (participants.Participant, bool, error) {
	people, err := s.o.Directory.Participants(ctx, groupID)
	if err != nil {
		return participants.Participant{}, false, err
	}
	for _, p := range people {
		if p.ID == id {
			return p, true, nil
		}
	}
	return participants.Participant{}, false, nil
}

// HandleUpdated traite « lettre nouvelle ou modifiée » : un courriel regroupé
// au tireur, seulement s'il peut déjà lire la lettre (tirage fait et
// révélation ouverte).
func (s *Service) HandleUpdated(ctx context.Context, e ports.Event) error {
	rid, err := strconv.ParseInt(e.Payload[letter.PayloadRecipientID], 10, 64)
	if err != nil {
		return errs.Validation("Événement de lettre invalide.")
	}
	lid, _ := strconv.ParseInt(e.Payload[letter.PayloadLetterID], 10, 64)
	giver, targets, err := s.giverOf(ctx, e.GroupID, rid)
	if err != nil || giver == 0 {
		return err // pas de tirage : personne à prévenir
	}
	open, err := s.o.Gate.IsOpen(ctx, e.GroupID)
	if err != nil {
		return errs.Internal(err)
	}
	if !open {
		return nil
	}
	p, ok, err := s.person(ctx, e.GroupID, giver)
	if err != nil || !ok {
		return err
	}
	if p.Role == participants.RoleReceptionSeule || strings.TrimSpace(p.Email) == "" {
		return nil
	}
	if sup, err := s.o.Suppress.IsSuppressed(ctx, e.GroupID, p.Email); err != nil {
		return errs.Internal(err)
	} else if sup {
		return nil
	}
	if ok, err := s.o.Unsub.ShouldSend(ctx, templates.LetterUpdated, p.ID); err != nil {
		return errs.Internal(err)
	} else if !ok {
		return nil
	}
	g, err := s.o.Directory.Group(ctx, e.GroupID)
	if err != nil {
		return err
	}
	d := templates.Data{
		To: p.Email, FirstName: p.FirstName, GroupName: g.Name,
		ParticipantLang: p.Locale, GroupLang: g.Lang, Link: s.o.LinkFor(g, p),
	}
	if targets == 1 { // avec deux lettres, aucun décompte : le message reste le même
		n, err := s.ideaCount(ctx, lid)
		if err != nil {
			return err
		}
		d.Ideas = n
	}
	d, err = s.o.Unsub.Decorate(templates.LetterUpdated, g.ID, p.ID, d)
	if err != nil {
		return errs.Internal(err)
	}
	msg, err := templates.Render(s.o.Catalog, templates.LetterUpdated, d)
	if err != nil {
		return errs.Internal(err)
	}
	if _, err := s.o.Queue.EnqueueMail(ctx, queue.Item{
		GroupID: g.ID, ParticipantID: p.ID, To: p.Email, ToName: p.FirstName,
		Type: mailunsub.TypeNouvelleIdee, Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML,
		UnsubscribeURL: d.UnsubscribeURL, CoalesceKey: CoalesceKey(g.ID, p.ID),
	}); err != nil {
		return err
	}
	if s.o.Letters != nil && lid > 0 {
		if err := s.o.Letters.MarkNotified(ctx, lid); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ideaCount(ctx context.Context, letterID int64) (int, error) {
	var n int
	if err := s.o.DB.QueryRowContext(ctx, `SELECT count(*) FROM letter_ideas WHERE letter_id = ?`, letterID).Scan(&n); err != nil {
		return 0, errs.Internal(err)
	}
	return n, nil
}

// TickResult compte ce qu'un passage a fait.
type TickResult struct{ Queued int }

// unreadInfo résume les lettres non ouvertes d'un tireur.
type unreadInfo struct {
	letters int
	trigger time.Time // le plus récent : lettre modifiée, tirage ou ouverture de la révélation
}

// unread liste, par tireur, les lettres nouvelles ou modifiées non ouvertes
// (cachet de F5b absent ou périmé) du tirage actuel.
func (s *Service) unread(ctx context.Context, groupID int64) (map[int64]*unreadInfo, error) {
	rows, err := s.o.DB.QueryContext(ctx, `SELECT p.giver_id, l.updated_at, d.drawn_at, g.reveal_at, o.opened_at
		FROM draw_pairs p
		JOIN draws d ON d.id = p.draw_id AND d.group_id = ? AND d.status = 'actuel'
		JOIN groups g ON g.id = d.group_id
		JOIN letters l ON l.participant_id = p.receiver_id AND l.group_id = d.group_id AND l.version >= 1
		LEFT JOIN letterread_stamps s ON s.letter_id = l.id AND s.pair_id = p.id AND s.version = l.version
		LEFT JOIN reveal_openings o ON o.draw_id = d.id
		WHERE s.letter_id IS NULL`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	out := map[int64]*unreadInfo{}
	for rows.Next() {
		var giver int64
		var upd, drawn string
		var reveal, opened sql.NullString
		if err := rows.Scan(&giver, &upd, &drawn, &reveal, &opened); err != nil {
			return nil, errs.Internal(err)
		}
		in := out[giver]
		if in == nil {
			in = &unreadInfo{}
			out[giver] = in
		}
		in.letters++
		for _, v := range []sql.NullString{{String: upd, Valid: true}, {String: drawn, Valid: true}, reveal, opened} {
			if !v.Valid {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, v.String)
			if err != nil {
				return nil, errs.Internal(err)
			}
			if t.After(in.trigger) {
				in.trigger = t
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// remindersSince compte les relances automatiques du tireur envoyées depuis
// l'instant donné : une lettre modifiée repart de zéro.
func (s *Service) remindersSince(ctx context.Context, groupID, giverID int64, since time.Time) (int, error) {
	rows, err := s.o.DB.QueryContext(ctx, `SELECT sent_at FROM reminders_sent
		WHERE group_id = ? AND participant_id = ? AND kind = ? AND automatic = 1`, groupID, giverID, kindReminderLog)
	if err != nil {
		return 0, errs.Internal(err)
	}
	defer rows.Close()
	n := 0
	since = since.Truncate(time.Second)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return 0, errs.Internal(err)
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return 0, errs.Internal(err)
		}
		if !t.Before(since) {
			n++
		}
	}
	return n, rows.Err()
}

// Tick relance les tireurs qui n'ont pas ouvert une lettre nouvelle ou
// modifiée : une seule relance par tireur, quel que soit le nombre de lettres
// non ouvertes, avec les garde-fous de F7 (fenêtre d'envoi, un courriel de
// relance par jour, plafond par type). L'ouverture est relue à chaque passage :
// elle arrête la relance. À appeler régulièrement avec l'horloge de F1.
func (s *Service) Tick(ctx context.Context) (TickResult, error) {
	var res TickResult
	now := s.o.Clock.Now()
	if !s.o.Rules.InWindow(now) {
		return res, nil
	}
	ids, err := s.o.Directory.GroupIDs(ctx)
	if err != nil {
		return res, err
	}
	for _, gid := range ids {
		n, err := s.tickGroup(ctx, gid, now)
		res.Queued += n
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Service) tickGroup(ctx context.Context, gid int64, now time.Time) (int, error) {
	st, err := s.o.Rules.Settings(ctx, gid)
	if err != nil || !st.Enabled {
		return 0, err
	}
	open, err := s.o.Gate.IsOpen(ctx, gid)
	if err != nil {
		return 0, errs.Internal(err)
	}
	if !open {
		return 0, nil
	}
	un, err := s.unread(ctx, gid)
	if err != nil || len(un) == 0 {
		return 0, err
	}
	g, err := s.o.Directory.Group(ctx, gid)
	if err != nil {
		return 0, err
	}
	people, err := s.o.Directory.Participants(ctx, gid)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, p := range people {
		in := un[p.ID]
		if in == nil || p.Role == participants.RoleReceptionSeule || strings.TrimSpace(p.Email) == "" {
			continue
		}
		count, err := s.remindersSince(ctx, gid, p.ID, in.trigger)
		if err != nil {
			return queued, err
		}
		due, ok := s.o.Rules.DueAt(st, in.trigger, count)
		if !ok || now.Before(due) {
			continue
		}
		sent, err := s.remind(ctx, g, p)
		if err != nil {
			return queued, err
		}
		if sent {
			queued++
		}
	}
	return queued, nil
}

// remind met en file la relance unique du tireur : la mention qu'une lettre
// l'attend, sans nom ni décompte.
func (s *Service) remind(ctx context.Context, g reminders.GroupInfo, p participants.Participant) (bool, error) {
	if sup, err := s.o.Suppress.IsSuppressed(ctx, g.ID, p.Email); err != nil {
		return false, errs.Internal(err)
	} else if sup {
		return false, nil
	}
	// La désinscription « relances » coupe cette relance (modèle de rappel de F6).
	if ok, err := s.o.Unsub.ShouldSend(ctx, templates.ReminderReading, p.ID); err != nil {
		return false, errs.Internal(err)
	} else if !ok {
		return false, nil
	}
	d, err := s.o.Unsub.Decorate(templates.ReminderReading, g.ID, p.ID, templates.Data{
		To: p.Email, FirstName: p.FirstName, GroupName: g.Name,
		ParticipantLang: p.Locale, GroupLang: g.Lang, Link: s.o.LinkFor(g, p),
	})
	if err != nil {
		return false, errs.Internal(err)
	}
	msg, err := templates.Render(s.o.Catalog, templates.LetterUpdated, d)
	if err != nil {
		return false, errs.Internal(err)
	}
	claimed, err := s.o.Rules.Claim(ctx, g.ID, p.ID, kindReminderLog)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	if _, err := s.o.Queue.EnqueueMail(ctx, queue.Item{
		GroupID: g.ID, ParticipantID: p.ID, To: p.Email, ToName: p.FirstName,
		Type: mailunsub.TypeRelances, Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML,
		UnsubscribeURL: d.UnsubscribeURL,
	}); err != nil {
		_ = s.o.Rules.Release(ctx, g.ID, p.ID)
		return false, err
	}
	if _, err := s.o.DB.ExecContext(ctx, `INSERT INTO reminders_sent (group_id, participant_id, kind, automatic, sent_at)
		VALUES (?, ?, ?, 1, ?)`, g.ID, p.ID, kindReminderLog, s.o.Clock.Now().UTC().Format(time.RFC3339)); err != nil {
		return false, errs.Internal(err)
	}
	return true, nil
}
