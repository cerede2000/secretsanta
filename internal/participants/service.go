package participants

import (
	"context"
	"database/sql"
	"net/mail"
	"strings"
	"time"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/sanitize"
)

// ActorKind distingue qui agit.
type ActorKind int

const (
	// ActorAnonymous est l'absence d'authentification (valeur zéro).
	ActorAnonymous ActorKind = iota
	ActorAdmin
	ActorParticipant
)

// Actor est l'identité authentifiée (issue des sessions) qui appelle le service.
type Actor struct {
	Kind          ActorKind
	GroupID       int64
	ParticipantID int64
}

// AdminActor est l'administrateur du groupe.
func AdminActor(groupID int64) Actor { return Actor{Kind: ActorAdmin, GroupID: groupID} }

// ParticipantActor est le participant connecté.
func ParticipantActor(groupID, participantID int64) Actor {
	return Actor{Kind: ActorParticipant, GroupID: groupID, ParticipantID: participantID}
}

// Messages français.
const (
	msgAuth          = "Authentification requise."
	msgForbidden     = "Vous n'avez pas le droit d'effectuer cette action."
	msgNotFound      = "Ce participant n'existe pas."
	msgNameTaken     = "Une personne porte déjà ce nom dans le groupe (sans tenir compte des majuscules ni des accents)."
	msgEmail         = "L'adresse courriel n'est pas valide."
	msgLocale        = "Le code de langue n'est pas valide."
	msgRole          = "Ce rôle dans le tirage n'existe pas."
	msgConfirm       = "La suppression doit être confirmée."
	msgDeleteAfter   = "Le tirage est fait : la suppression passe par « Ajuster le tirage »."
	msgRoleLocked    = "Le tirage est fait : la préférence ne peut plus être changée par la personne elle-même ; contactez l'administrateur."
	msgRoleNotYours  = "Ce rôle a été fixé par l'administrateur : seul l'administrateur peut le changer."
	msgRoleSelfValue = "Vous pouvez seulement choisir entre offrir normalement et n'offrir qu'un seul cadeau."
)

// Service gère les participants.
type Service struct {
	db    *sql.DB
	clock clock.Clock
	draw  ports.DrawState
}

// NewService construit le service ; draw indique si le tirage est fait.
func NewService(d *sql.DB, c clock.Clock, draw ports.DrawState) *Service {
	return &Service{db: d, clock: c, draw: draw}
}

// Input est la saisie d'un participant.
type Input struct {
	Name      string
	FirstName string
	Email     string // facultatif
	GiftHint  string
	Locale    string   // facultatif
	Role      DrawRole // vide = normal
}

type cleaned struct {
	name, first, email, hint, locale string
}

func cleanInput(in Input) (cleaned, error) {
	var c cleaned
	var err error
	if c.name, err = sanitize.Required(in.Name, NameMaxLen, "Le nom"); err != nil {
		return c, err
	}
	if c.first, err = sanitize.Required(in.FirstName, FirstNameMaxLen, "Le prénom"); err != nil {
		return c, err
	}
	if c.email, err = sanitize.Line(in.Email, EmailMaxLen, "Le courriel"); err != nil {
		return c, err
	}
	if c.email != "" && !validEmail(c.email) {
		return c, errs.Validation(msgEmail)
	}
	if c.hint, err = sanitize.Text(in.GiftHint, GiftHintMaxLen, "L'indice cadeau"); err != nil {
		return c, err
	}
	if c.locale, err = cleanLocale(in.Locale); err != nil {
		return c, err
	}
	return c, nil
}

func cleanLocale(s string) (string, error) {
	l, err := sanitize.Line(s, LocaleMaxLen, "Le code de langue")
	if err != nil {
		return "", err
	}
	if l != "" && !ValidLocale(l) {
		return "", errs.Validation(msgLocale)
	}
	return l, nil
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" {
		return false
	}
	at := strings.LastIndexByte(s, '@')
	dom := s[at+1:]
	return at > 0 && strings.Contains(dom, ".") && !strings.HasPrefix(dom, ".") && !strings.HasSuffix(dom, ".")
}

func (a Actor) authenticatedIn(groupID int64) error {
	if a.Kind == ActorAnonymous {
		return errs.New(errs.KindUnauthorized, msgAuth)
	}
	if a.GroupID != groupID || groupID == 0 {
		return errs.New(errs.KindForbidden, msgForbidden)
	}
	return nil
}

func (a Actor) admin(groupID int64) error {
	if err := a.authenticatedIn(groupID); err != nil {
		return err
	}
	if a.Kind != ActorAdmin {
		return errs.New(errs.KindForbidden, msgForbidden)
	}
	return nil
}

func isUnique(err error) bool { return err != nil && strings.Contains(err.Error(), "UNIQUE") }

// Create ajoute un participant à la main (administrateur). joined_at est
// l'instant de création.
func (s *Service) Create(ctx context.Context, a Actor, groupID int64, in Input) (*Participant, error) {
	if err := a.admin(groupID); err != nil {
		return nil, err
	}
	return s.insert(ctx, groupID, in, s.clock.Now().UTC())
}

// Register ajoute un participant inscrit par lien (F11) : joined_at est
// l'instant de la vérification, fourni par l'appelant. Le rôle ne peut jamais
// être un_seul.
func (s *Service) Register(ctx context.Context, groupID int64, in Input, verifiedAt time.Time) (*Participant, error) {
	if in.Role == RoleUnSeul {
		return nil, errs.Validation(msgRoleSelfValue)
	}
	return s.insert(ctx, groupID, in, verifiedAt.UTC())
}

func (s *Service) insert(ctx context.Context, groupID int64, in Input, joined time.Time) (*Participant, error) {
	c, err := cleanInput(in)
	if err != nil {
		return nil, err
	}
	role := in.Role
	if role == "" {
		role = RoleNormal
	}
	if !role.Valid() {
		return nil, errs.Validation(msgRole)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	exists, err := NameExists(ctx, tx, groupID, c.first, c.name, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errs.Conflict(msgNameTaken)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO participants
		(group_id, name, first_name, name_key, email, gift_hint, draw_role, locale, joined_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		groupID, c.name, c.first, NormalizeName(c.first, c.name), c.email, c.hint, string(role), c.locale,
		joined.Format(time.RFC3339Nano))
	if err != nil {
		if isUnique(err) {
			return nil, errs.Conflict(msgNameTaken)
		}
		return nil, errs.Internal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		if isUnique(err) {
			return nil, errs.Conflict(msgNameTaken)
		}
		return nil, errs.Internal(err)
	}
	return &Participant{ID: id, GroupID: groupID, Name: c.name, FirstName: c.first, Email: c.email,
		GiftHint: c.hint, Role: role, Locale: c.locale, JoinedAt: joined}, nil
}

const selectCols = `id, group_id, name, first_name, email, gift_hint, draw_role, locale, joined_at`

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Participant, error) {
	var p Participant
	var role, joined string
	if err := r.Scan(&p.ID, &p.GroupID, &p.Name, &p.FirstName, &p.Email, &p.GiftHint, &role, &p.Locale, &joined); err != nil {
		return p, err
	}
	p.Role = DrawRole(role)
	t, err := time.Parse(time.RFC3339Nano, joined)
	if err != nil {
		return p, err
	}
	p.JoinedAt = t.UTC()
	return p, nil
}

func (s *Service) load(ctx context.Context, groupID, id int64) (*Participant, error) {
	p, err := scan(s.db.QueryRowContext(ctx,
		`SELECT `+selectCols+` FROM participants WHERE group_id = ? AND id = ?`, groupID, id))
	if err == sql.ErrNoRows {
		return nil, errs.NotFound(msgNotFound)
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	return &p, nil
}

// Get renvoie un participant : l'administrateur du groupe ou la personne elle-même.
func (s *Service) Get(ctx context.Context, a Actor, groupID, id int64) (*Participant, error) {
	if err := a.authenticatedIn(groupID); err != nil {
		return nil, err
	}
	if a.Kind == ActorParticipant && a.ParticipantID != id {
		return nil, errs.New(errs.KindForbidden, msgForbidden)
	}
	return s.load(ctx, groupID, id)
}

// List renvoie les participants d'un groupe, par ordre d'entrée. Jamais sans
// authentification, jamais depuis un autre groupe ; une personne connectée voit
// la vue publique via Public().
func (s *Service) List(ctx context.Context, a Actor, groupID int64) ([]Participant, error) {
	if err := a.authenticatedIn(groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+selectCols+` FROM participants WHERE group_id = ? ORDER BY joined_at, id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []Participant
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, errs.Internal(err)
		}
		if a.Kind == ActorParticipant {
			// Vue publique : rien d'autre que le nom.
			p = Participant{ID: p.ID, GroupID: p.GroupID, Name: p.Name, FirstName: p.FirstName}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// Update modifie nom, prénom, courriel et indice (administrateur), avant comme
// après le tirage. Le lien, le code, le rôle, la langue et la paire ne changent pas.
// Aucun courriel n'est envoyé.
func (s *Service) Update(ctx context.Context, a Actor, groupID, id int64, in Input) (*Participant, error) {
	if err := a.admin(groupID); err != nil {
		return nil, err
	}
	c, err := cleanInput(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM participants WHERE group_id = ? AND id = ?`, groupID, id).Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return nil, errs.NotFound(msgNotFound)
		}
		return nil, errs.Internal(err)
	}
	exists, err := NameExists(ctx, tx, groupID, c.first, c.name, id)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errs.Conflict(msgNameTaken)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE participants SET name = ?, first_name = ?, name_key = ?, email = ?, gift_hint = ?
		WHERE group_id = ? AND id = ?`,
		c.name, c.first, NormalizeName(c.first, c.name), c.email, c.hint, groupID, id); err != nil {
		if isUnique(err) {
			return nil, errs.Conflict(msgNameTaken)
		}
		return nil, errs.Internal(err)
	}
	if err := tx.Commit(); err != nil {
		if isUnique(err) {
			return nil, errs.Conflict(msgNameTaken)
		}
		return nil, errs.Internal(err)
	}
	return s.load(ctx, groupID, id)
}

// Delete supprime un participant : confirmation obligatoire, refus après
// tirage (renvoi vers « Ajuster le tirage »).
func (s *Service) Delete(ctx context.Context, a Actor, groupID, id int64, confirmed bool) error {
	if err := a.admin(groupID); err != nil {
		return err
	}
	if !confirmed {
		return errs.Validation(msgConfirm)
	}
	done, err := s.draw.IsDrawDone(ctx, groupID)
	if err != nil {
		return errs.Internal(err)
	}
	if done {
		return errs.Conflict(msgDeleteAfter)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM participants WHERE group_id = ? AND id = ?`, groupID, id)
	if err != nil {
		return errs.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.NotFound(msgNotFound)
	}
	return nil
}

// SetRole règle le rôle dans le tirage. L'administrateur : toute valeur, à tout
// moment. La personne : elle-même, avant le tirage, entre normal et un_seul.
func (s *Service) SetRole(ctx context.Context, a Actor, groupID, id int64, role DrawRole) error {
	if err := a.authenticatedIn(groupID); err != nil {
		return err
	}
	if !role.Valid() {
		return errs.Validation(msgRole)
	}
	p, err := s.load(ctx, groupID, id)
	if err != nil {
		return err
	}
	if a.Kind == ActorParticipant {
		if a.ParticipantID != id {
			return errs.New(errs.KindForbidden, msgForbidden)
		}
		done, err := s.draw.IsDrawDone(ctx, groupID)
		if err != nil {
			return errs.Internal(err)
		}
		if done {
			return errs.Conflict(msgRoleLocked)
		}
		if p.Role != RoleNormal && p.Role != RoleUnSeul {
			return errs.New(errs.KindForbidden, msgRoleNotYours)
		}
		if role != RoleNormal && role != RoleUnSeul {
			return errs.Validation(msgRoleSelfValue)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE participants SET draw_role = ? WHERE group_id = ? AND id = ?`,
		string(role), groupID, id); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// SetLocale enregistre la langue du participant (port ParticipantLocale de F13) ;
// vide = langue par défaut du groupe. Modifiable par l'administrateur et par la
// personne elle-même.
func (s *Service) SetLocale(ctx context.Context, a Actor, groupID, id int64, locale string) error {
	if err := a.authenticatedIn(groupID); err != nil {
		return err
	}
	if a.Kind == ActorParticipant && a.ParticipantID != id {
		return errs.New(errs.KindForbidden, msgForbidden)
	}
	l, err := cleanLocale(locale)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE participants SET locale = ? WHERE group_id = ? AND id = ?`, l, groupID, id)
	if err != nil {
		return errs.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.NotFound(msgNotFound)
	}
	return nil
}

// CanDraw indique si le groupe a le minimum de participants requis.
func (s *Service) CanDraw(ctx context.Context, groupID int64) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants WHERE group_id = ?`, groupID).Scan(&n); err != nil {
		return false, errs.Internal(err)
	}
	return n >= MinParticipants, nil
}

// RosterEntry est une ligne du port de lecture : aucun nom.
type RosterEntry struct {
	ID   int64
	Role DrawRole
}

// RoleCounts sont les décomptes par rôle. Bénéficiaires : toutes les personnes
// (elles reçoivent toutes un cadeau) ; tireurs : toutes sauf reception_seule.
type RoleCounts struct {
	Beneficiaries  int
	Givers         int
	ReceptionSeule int
	OffrePlus      int
	Normal         int
	UnSeul         int
}

// Roster est le port de lecture pour F4, sans aucun nom.
type Roster struct {
	Entries []RosterEntry
	Counts  RoleCounts
}

// Roster liste les participants avec leur rôle et les décomptes (appel interne
// de F4, sans nom ni courriel).
func (s *Service) Roster(ctx context.Context, groupID int64) (*Roster, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, draw_role FROM participants WHERE group_id = ? ORDER BY id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	r := &Roster{}
	for rows.Next() {
		var e RosterEntry
		var role string
		if err := rows.Scan(&e.ID, &role); err != nil {
			return nil, errs.Internal(err)
		}
		e.Role = DrawRole(role)
		r.Entries = append(r.Entries, e)
		r.Counts.Beneficiaries++
		switch e.Role {
		case RoleReceptionSeule:
			r.Counts.ReceptionSeule++
		case RoleOffrePlus:
			r.Counts.Givers++
			r.Counts.OffrePlus++
		case RoleUnSeul:
			r.Counts.Givers++
			r.Counts.UnSeul++
		default:
			r.Counts.Givers++
			r.Counts.Normal++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return r, nil
}
