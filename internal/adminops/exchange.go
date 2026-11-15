package adminops

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/sanitize"
)

// Format et version du fichier d'échange des participants.
const (
	ExchangeFormat  = "secretsanta.participants"
	ExchangeVersion = 1
)

// Bornes de l'import.
const (
	// MaxImportBytes borne la taille du fichier importé.
	MaxImportBytes = 1 << 20
	// MaxImportParticipants borne le nombre de personnes d'un fichier.
	MaxImportParticipants = 1000
	// householdNameMax est la borne du nom d'un foyer (table households).
	householdNameMax = 80
)

// Codes de message (clés du catalogue adminops) des refus d'import.
const (
	CodeImportTooLarge = "adminops.error.import_too_large"
	CodeImportFormat   = "adminops.error.import_format"
	CodeImportField    = "adminops.error.import_field"
	CodeImportEmail    = "adminops.error.import_email"
	CodeImportLocale   = "adminops.error.import_locale"
	CodeImportRole     = "adminops.error.import_role"
	CodeImportName     = "adminops.error.import_name"
	CodeImportHouse    = "adminops.error.import_household"
	CodeImportEmpty    = "adminops.error.import_empty"
)

// ExchangeFile est le fichier JSON d'échange des participants. Il ne contient
// jamais de paire du tirage, d'identifiant, de lien ni de code d'accès.
type ExchangeFile struct {
	Format       string              `json:"format"`
	Version      int                 `json:"version"`
	Participants []ExchangePerson    `json:"participants"`
	Households   []ExchangeHousehold `json:"households"`
}

// ExchangePerson est une personne du fichier. DrawRole absent (nil) vaut
// « normal » à l'import ; l'export l'écrit toujours.
type ExchangePerson struct {
	FirstName string  `json:"first_name"`
	Name      string  `json:"name"`
	Email     string  `json:"email,omitempty"`
	GiftHint  string  `json:"gift_hint,omitempty"`
	Locale    string  `json:"locale,omitempty"`
	DrawRole  *string `json:"draw_role,omitempty"`
}

// ExchangeHousehold est un foyer : Members sont les positions (à partir de 0)
// des personnes dans Participants.
type ExchangeHousehold struct {
	Name    string `json:"name"`
	Members []int  `json:"members"`
}

// ImportResult résume un import réussi.
type ImportResult struct {
	Participants int
	Households   int
}

// Export renvoie le fichier d'échange des participants du groupe, dans
// l'ordre d'entrée, avec leurs foyers.
func (s *Service) Export(ctx context.Context, groupID int64) (*ExchangeFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, first_name, name, email, gift_hint, locale, draw_role
		FROM participants WHERE group_id = ? ORDER BY joined_at, id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	f := &ExchangeFile{Format: ExchangeFormat, Version: ExchangeVersion,
		Participants: []ExchangePerson{}, Households: []ExchangeHousehold{}}
	pos := map[int64]int{}
	for rows.Next() {
		var id int64
		var p ExchangePerson
		var role string
		if err := rows.Scan(&id, &p.FirstName, &p.Name, &p.Email, &p.GiftHint, &p.Locale, &role); err != nil {
			return nil, errs.Internal(err)
		}
		p.DrawRole = &role
		pos[id] = len(f.Participants)
		f.Participants = append(f.Participants, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	hrows, err := s.db.QueryContext(ctx, `SELECT h.id, h.name, m.participant_id FROM households h
		LEFT JOIN household_members m ON m.household_id = h.id
		WHERE h.group_id = ? ORDER BY h.id, m.participant_id`, groupID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer hrows.Close()
	index := map[int64]int{}
	for hrows.Next() {
		var hid int64
		var name string
		var member sql.NullInt64
		if err := hrows.Scan(&hid, &name, &member); err != nil {
			return nil, errs.Internal(err)
		}
		i, ok := index[hid]
		if !ok {
			i = len(f.Households)
			index[hid] = i
			f.Households = append(f.Households, ExchangeHousehold{Name: name, Members: []int{}})
		}
		if member.Valid {
			if p, ok := pos[member.Int64]; ok {
				f.Households[i].Members = append(f.Households[i].Members, p)
			}
		}
	}
	if err := hrows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return f, nil
}

// ExportJSON écrit le fichier d'échange en JSON indenté.
func (s *Service) ExportJSON(ctx context.Context, groupID int64, w io.Writer) error {
	f, err := s.Export(ctx, groupID)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return errs.Internal(err)
	}
	return nil
}

func importErr(code, msg string, pairs ...any) error {
	return errs.Validation(msg).WithCode(code, pairs...)
}

// decodeExchange lit le fichier en refusant tout champ inconnu, toute donnée
// après l'objet et tout fichier trop gros.
func decodeExchange(r io.Reader) (*ExchangeFile, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxImportBytes+1))
	if err != nil {
		return nil, errs.Internal(err)
	}
	if len(raw) > MaxImportBytes {
		return nil, importErr(CodeImportTooLarge, "Le fichier est trop gros.")
	}
	bad := func() error {
		return importErr(CodeImportFormat, "Le fichier n'est pas un export de participants valide.")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f ExchangeFile
	if err := dec.Decode(&f); err != nil {
		return nil, bad()
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, bad()
	}
	if f.Format != ExchangeFormat || f.Version != ExchangeVersion || f.Participants == nil {
		return nil, bad()
	}
	if len(f.Participants) == 0 {
		return nil, importErr(CodeImportEmpty, "Le fichier ne contient aucun participant.")
	}
	if len(f.Participants) > MaxImportParticipants || len(f.Households) > MaxImportParticipants {
		return nil, importErr(CodeImportTooLarge, "Le fichier est trop gros.")
	}
	return &f, nil
}

type cleanPerson struct {
	first, name, email, hint, locale string
	role                             participants.DrawRole
}

// validEmail applique la règle de F3 (participants) : adresse nue, domaine
// pointé.
func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" {
		return false
	}
	at := strings.LastIndexByte(s, '@')
	dom := s[at+1:]
	return at > 0 && strings.Contains(dom, ".") && !strings.HasPrefix(dom, ".") && !strings.HasSuffix(dom, ".")
}

// cleanExchangePerson nettoie une personne avec les bornes de F3 ; n est son
// numéro (à partir de 1) pour les messages.
func cleanExchangePerson(p ExchangePerson, n int) (cleanPerson, error) {
	var c cleanPerson
	var err error
	field := func() error {
		return importErr(CodeImportField, fmt.Sprintf("Personne n° %d : un champ est vide ou trop long.", n), "n", n)
	}
	if c.first, err = sanitize.Required(p.FirstName, participants.FirstNameMaxLen, "Le prénom"); err != nil {
		return c, field()
	}
	if c.name, err = sanitize.Required(p.Name, participants.NameMaxLen, "Le nom"); err != nil {
		return c, field()
	}
	if c.email, err = sanitize.Line(p.Email, participants.EmailMaxLen, "Le courriel"); err != nil {
		return c, field()
	}
	if c.email != "" && !validEmail(c.email) {
		return c, importErr(CodeImportEmail, fmt.Sprintf("Personne n° %d : l'adresse courriel n'est pas valide.", n), "n", n)
	}
	if c.hint, err = sanitize.Text(p.GiftHint, participants.GiftHintMaxLen, "L'indice cadeau"); err != nil {
		return c, field()
	}
	if c.locale, err = sanitize.Line(p.Locale, participants.LocaleMaxLen, "Le code de langue"); err != nil {
		return c, field()
	}
	if c.locale != "" && !participants.ValidLocale(c.locale) {
		return c, importErr(CodeImportLocale, fmt.Sprintf("Personne n° %d : le code de langue n'est pas valide.", n), "n", n)
	}
	c.role = participants.RoleNormal
	if p.DrawRole != nil {
		c.role = participants.DrawRole(*p.DrawRole)
		if !c.role.Valid() {
			return c, importErr(CodeImportRole, fmt.Sprintf("Personne n° %d : ce rôle dans le tirage n'existe pas.", n), "n", n)
		}
	}
	return c, nil
}

// Import ajoute au groupe les participants et les foyers du fichier, dans une
// seule transaction : la moindre erreur (schéma, champ, rôle inconnu, nom
// déjà pris dans le groupe ou dans le fichier, foyer incohérent) n'importe
// rien. Les noms suivent la règle d'unicité de F3 (NormalizeName, NameExists).
func (s *Service) Import(ctx context.Context, groupID int64, r io.Reader) (*ImportResult, error) {
	f, err := decodeExchange(r)
	if err != nil {
		return nil, err
	}
	people := make([]cleanPerson, len(f.Participants))
	for i, p := range f.Participants {
		if people[i], err = cleanExchangePerson(p, i+1); err != nil {
			return nil, err
		}
	}
	type house struct {
		name    string
		members []int
	}
	houses := make([]house, len(f.Households))
	seen := map[int]bool{}
	for i, h := range f.Households {
		n := i + 1
		bad := func() error {
			return importErr(CodeImportHouse, fmt.Sprintf("Foyer n° %d : le nom ou les membres ne sont pas valides.", n), "n", n)
		}
		name, err := sanitize.Required(h.Name, householdNameMax, "Le nom du foyer")
		if err != nil || len(h.Members) == 0 {
			return nil, bad()
		}
		for _, m := range h.Members {
			if m < 0 || m >= len(people) || seen[m] {
				return nil, bad()
			}
			seen[m] = true
		}
		houses[i] = house{name: name, members: h.Members}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM groups WHERE id = ?`, groupID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NotFound("Ce groupe n'existe pas.")
		}
		return nil, errs.Internal(err)
	}
	// Un même instant pour tous : l'ordre du fichier est gardé par
	// l'identifiant croissant (tri joined_at, id).
	joined := s.clock.Now().UTC().Format(time.RFC3339Nano)
	ids := make([]int64, len(people))
	for i, c := range people {
		n := i + 1
		taken := func() error {
			return importErr(CodeImportName, fmt.Sprintf("Personne n° %d : ce nom est déjà pris dans le groupe ou dans le fichier.", n), "n", n)
		}
		exists, err := participants.NameExists(ctx, tx, groupID, c.first, c.name, 0)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, taken()
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO participants
			(group_id, name, first_name, name_key, email, gift_hint, draw_role, locale, joined_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			groupID, c.name, c.first, participants.NormalizeName(c.first, c.name), c.email, c.hint,
			string(c.role), c.locale, joined)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return nil, taken()
			}
			return nil, errs.Internal(err)
		}
		if ids[i], err = res.LastInsertId(); err != nil {
			return nil, errs.Internal(err)
		}
	}
	for _, h := range houses {
		res, err := tx.ExecContext(ctx, `INSERT INTO households(group_id, name) VALUES (?, ?)`, groupID, h.name)
		if err != nil {
			return nil, errs.Internal(err)
		}
		hid, err := res.LastInsertId()
		if err != nil {
			return nil, errs.Internal(err)
		}
		for _, m := range h.members {
			if _, err := tx.ExecContext(ctx, `INSERT INTO household_members(participant_id, household_id) VALUES (?, ?)`,
				ids[m], hid); err != nil {
				return nil, errs.Internal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, errs.Internal(err)
	}
	return &ImportResult{Participants: len(people), Households: len(houses)}, nil
}
