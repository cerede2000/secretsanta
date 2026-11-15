package app

import (
	"context"
	"database/sql"
	"embed"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/adjust"
	"secretsanta/internal/db"
	"secretsanta/internal/letterread"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
)

// AppModule est le module de migrations propre à l'intégration : les tables
// qui ne relèvent d'aucun paquet de fonctionnalité (archives des lettres des
// personnes retirées du tirage).
const AppModule = "app"

//go:embed sql/*.sql
var appSQL embed.FS

// AppMigrations renvoie les migrations du module « app ».
func AppMigrations() ([]db.Migration, error) { return db.LoadFS(appSQL, "sql") }

// Adjusting est l'ajustement du tirage (retrait ou ajout d'une personne) tel
// que les pages le consomment : le service adjust, plus la mise à jour de la
// révélation une fois l'ajustement enregistré (cibles à ouvrir, avis). Appeler
// directement adjust.Service.Apply contournerait la révélation : passer par
// ce type, comme Drawing pour les tirages.
type Adjusting struct {
	svc     *adjust.Service
	drawing *Drawing
}

// Preview calcule l'aperçu d'un ajustement sans rien modifier (comptes
// seulement, aucun nom).
func (d *Adjusting) Preview(ctx context.Context, a participants.Actor, groupID int64, ch adjust.Change) (*adjust.Preview, error) {
	return d.svc.Preview(ctx, a, groupID, ch)
}

// Apply applique l'ajustement confirmé (voir adjust.Service.Apply), puis met la
// révélation à jour. L'ajustement est déjà enregistré quand la révélation est
// prévenue : une panne de celle-ci est journalisée et retentée, elle ne défait
// rien.
func (d *Adjusting) Apply(ctx context.Context, a participants.Actor, groupID int64, ch adjust.Change, c adjust.Confirmation) (*adjust.Result, error) {
	res, err := d.svc.Apply(ctx, a, groupID, ch, c)
	if err != nil {
		return nil, err
	}
	d.drawing.changed(ctx, groupID)
	return res, nil
}

// letterArchiver adapte letterread.Service au port adjust.Archiver. Le port de
// l'ajustement archive « la lettre de l'ancienne cible d'un tireur » (une
// paire à la fois) ; celui de letterread archive les lettres d'une liste de
// tirés. Seul le tiré compte : le tireur n'entre jamais dans l'archive.
type letterArchiver struct{ svc *letterread.Service }

// ArchiveTx implémente adjust.Archiver.
func (l letterArchiver) ArchiveTx(ctx context.Context, tx *sql.Tx, groupID, _, oldReceiverID int64) error {
	return l.svc.ArchiveTx(ctx, tx, groupID, []int64{oldReceiverID})
}

var _ adjust.Archiver = letterArchiver{}

// participantRemover implémente adjust.Remover : il supprime le participant et
// tout ce qui le visait, dans la transaction de l'ajustement.
//
// Décision d'archive. La suppression du participant cascade sur sa lettre puis
// sur letter_archives. L'archive (instantané de la lettre de l'ancienne cible,
// posé par letterread dans cette même transaction) est donc COPIÉE dans
// removed_letter_archives, table sans clé étrangère vers les participants,
// avant la suppression. Le participant est bien supprimé (il disparaît des
// listes et ne peut plus se connecter) et l'archive survit. Les fichiers
// d'image des idées restent sur le disque, car l'instantané les nomme.
//
// Accès révoqués. Les identifiants de participants (INTEGER PRIMARY KEY, sans
// AUTOINCREMENT) peuvent être réattribués : tout ce qui est rattaché à
// l'identifiant est donc supprimé avec la personne (sessions, lien personnel et
// code, liens magiques, courriels en attente, préférences de désinscription,
// relances, marque « lien remis »), pour qu'une personne ajoutée plus tard
// n'en hérite jamais.
type participantRemover struct{ clock clock.Clock }

// RemoveTx implémente adjust.Remover.
func (r participantRemover) RemoveTx(ctx context.Context, tx *sql.Tx, groupID, participantID int64) error {
	at := r.clock.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO removed_letter_archives
		(group_id, participant_id, version, snapshot, archived_at, removed_at)
		SELECT l.group_id, l.participant_id, a.version, a.snapshot, a.archived_at, ?
		FROM letter_archives a JOIN letters l ON l.id = a.letter_id
		WHERE l.group_id = ? AND l.participant_id = ? ORDER BY a.id`, at, groupID, participantID); err != nil {
		return errs.Internal(err)
	}
	gid, pid := strconv.FormatInt(groupID, 10), strconv.FormatInt(participantID, 10)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM household_members WHERE participant_id = ? AND household_id IN (SELECT id FROM households WHERE group_id = ?)`, []any{participantID, groupID}},
		{`DELETE FROM manual_exclusions WHERE group_id = ? AND (giver_id = ? OR receiver_id = ?)`, []any{groupID, participantID, participantID}},
		{`DELETE FROM forced_pairs WHERE group_id = ? AND (giver_id = ? OR receiver_id = ?)`, []any{groupID, participantID, participantID}},
		{`DELETE FROM websec_sessions WHERE kind = 'participant' AND group_id = ? AND subject = ?`, []any{gid, pid}},
		{`DELETE FROM access_credentials WHERE group_id = ? AND participant_id = ?`, []any{gid, pid}},
		{`DELETE FROM magic_links WHERE group_id = ? AND participant_id = ?`, []any{groupID, participantID}},
		{`DELETE FROM mail_queue WHERE group_id = ? AND participant_id = ? AND status = 'pending'`, []any{groupID, participantID}},
		{`DELETE FROM reveal_notices WHERE group_id = ? AND participant_id = ? AND sent_at IS NULL`, []any{groupID, participantID}},
		{`DELETE FROM mail_unsubscribe WHERE participant_id = ?`, []any{participantID}},
		{`DELETE FROM reminder_daily_sends WHERE group_id = ? AND participant_id = ?`, []any{groupID, participantID}},
		{`DELETE FROM admin_link_handover WHERE group_id = ? AND participant_id = ?`, []any{groupID, participantID}},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			return errs.Internal(err)
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM participants WHERE id = ? AND group_id = ?`, participantID, groupID)
	if err != nil {
		return errs.Internal(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return errs.Internal(err)
	} else if n != 1 {
		return errs.NotFound("Cette personne n'existe pas dans le groupe.")
	}
	return nil
}

var _ adjust.Remover = participantRemover{}

// adjustComposer écrit le courriel d'ajustement avec le modèle DrawAdjusted du
// catalogue : il ne nomme que le destinataire. Le lien mène à /<slug>/participant,
// qui renvoie vers la découverte ou la carte de cible, ou vers la connexion si
// la personne n'est pas connectée (son lien personnel d'origine n'est
// conservé que sous forme d'empreinte).
func (a *App) adjustComposer(ctx context.Context, n adjust.Notice) (ports.EmailMessage, error) {
	g, err := a.Groups.ByID(ctx, n.GroupID)
	if err != nil {
		return ports.EmailMessage{}, err
	}
	first := strings.TrimSpace(n.Contact.FirstName)
	if first == "" {
		first = localPart(n.Contact.Email)
	}
	d := templates.Data{
		To: n.Contact.Email, FirstName: first, ParticipantLang: n.Contact.Locale,
		GroupName: g.Name, Link: a.publicBase() + "/" + g.Slug + "/participant",
	}
	if l, ok := a.GroupSettings.GroupLanguage(ctx, g.Slug); ok {
		d.GroupLang = l
	}
	m, err := templates.Render(a.Cat, templates.DrawAdjusted, d)
	if err != nil {
		return ports.EmailMessage{}, err
	}
	return ports.EmailMessage{
		To: m.To, Subject: m.Subject, Body: m.Text, Type: ports.EmailType(templates.DrawAdjusted), Essential: m.Essential,
	}, nil
}

// registerAdjust construit l'ajustement du tirage : lecture du tirage, des
// rôles, des règles et du plafond comme pour le tirage, avis par la file
// d'envoi, archivage des lettres des anciennes cibles par letterread et retrait
// de la personne (participantRemover). Aucune page (F10c, vague 9) : les pages
// utiliseront App.Adjust. Elle suit registerLetterRead et registerReveal.
func registerAdjust(a *App) error {
	svc := adjust.NewService(a.DB, adjust.Deps{
		Draw: a.Draw, Groups: a.Groups, Roster: a.Participants, Constraints: a.Rules,
		Directory: adjust.ParticipantDirectory{Participants: a.Participants},
		Mail:      a.mail, Archiver: letterArchiver{svc: a.LetterRead},
		Remover: participantRemover{clock: a.Clock}, Compose: a.adjustComposer,
	})
	a.Adjust = &Adjusting{svc: svc, drawing: a.Drawing}
	return nil
}
