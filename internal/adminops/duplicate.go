package adminops

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"secretsanta/internal/groups"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/random"
)

// CodeDuplicateSlug est le code du refus quand aucune adresse libre n'est
// trouvée pour le groupe dupliqué.
const CodeDuplicateSlug = "adminops.error.duplicate_slug"

// DuplicatePlan est ce que la duplication créera : nom, adresse et année.
type DuplicatePlan struct {
	Name string
	Slug string
	Year int
}

// DuplicateResult est renvoyé une seule fois : AdminSecret n'est stocké nulle
// part en clair.
type DuplicateResult struct {
	GroupID     int64
	Name        string
	Slug        string
	Year        int
	AdminSecret string
}

var yearRe = regexp.MustCompile(`(19|20|21)\d\d`)

type sourceGroup struct {
	slug, name, instructions string
	budget                   sql.NullInt64
	eventDate, revealAt      sql.NullString
	canView, maxGifts        int
}

func loadSource(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, groupID int64) (*sourceGroup, error) {
	var g sourceGroup
	err := q.QueryRowContext(ctx, `SELECT slug, name, budget_cents, gift_instructions, event_date, reveal_at,
		admin_can_view_pair, max_gifts_per_giver FROM groups WHERE id = ?`, groupID).
		Scan(&g.slug, &g.name, &g.budget, &g.instructions, &g.eventDate, &g.revealAt, &g.canView, &g.maxGifts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.NotFound("Ce groupe n'existe pas.")
	}
	if err != nil {
		return nil, errs.Internal(err)
	}
	return &g, nil
}

// baseYear est l'année du groupe source : celle du jour J, sinon une année
// écrite dans le nom ou l'adresse, sinon l'année courante.
func (s *Service) baseYear(g *sourceGroup) int {
	if g.eventDate.Valid {
		if t, err := time.Parse(groups.DateLayout, g.eventDate.String); err == nil {
			return t.Year()
		}
	}
	for _, v := range []string{g.name, g.slug} {
		if m := yearRe.FindAllString(v, -1); len(m) > 0 {
			y, _ := strconv.Atoi(m[len(m)-1])
			return y
		}
	}
	return s.clock.Now().Year()
}

// replaceLast remplace la dernière occurrence de old dans s.
func replaceLast(s, old, repl string) (string, bool) {
	i := strings.LastIndex(s, old)
	if i < 0 {
		return s, false
	}
	return s[:i] + repl + s[i+len(old):], true
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n]))
}

func nextName(name string, from, to int) string {
	if v, ok := replaceLast(name, strconv.Itoa(from), strconv.Itoa(to)); ok && utf8.RuneCountInString(v) <= groups.NameMaxLen {
		return v
	}
	suffix := " " + strconv.Itoa(to)
	return truncateRunes(name, groups.NameMaxLen-len(suffix)) + suffix
}

func nextSlugBase(slug string, from, to int) string {
	if v, ok := replaceLast(slug, strconv.Itoa(from), strconv.Itoa(to)); ok {
		return v
	}
	suffix := "-" + strconv.Itoa(to)
	base := slug
	if len(base)+len(suffix) > groups.SlugMaxLen {
		base = strings.TrimRight(base[:groups.SlugMaxLen-len(suffix)], "-")
	}
	return base + suffix
}

func slugFree(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, slug string) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE slug = ?`, slug).Scan(&n); err != nil {
		return false, errs.Internal(err)
	}
	return n == 0, nil
}

// pickSlug renvoie la première adresse valide et libre : base, puis base-2,
// base-3… (bornée).
func pickSlug(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, base string) (string, error) {
	for i := 1; i <= 50; i++ {
		cand := base
		if i > 1 {
			suffix := "-" + strconv.Itoa(i)
			b := base
			if len(b)+len(suffix) > groups.SlugMaxLen {
				b = strings.TrimRight(b[:groups.SlugMaxLen-len(suffix)], "-")
			}
			cand = b + suffix
		}
		v, err := groups.ValidateSlug(cand)
		if err != nil {
			continue
		}
		free, err := slugFree(ctx, q, v)
		if err != nil {
			return "", err
		}
		if free {
			return v, nil
		}
	}
	return "", errs.Conflict("Aucune adresse libre n'a été trouvée pour le nouveau groupe.").WithCode(CodeDuplicateSlug)
}

// PlanDuplicate calcule le nom, l'adresse et l'année du groupe que créerait
// Duplicate, sans rien écrire.
func (s *Service) PlanDuplicate(ctx context.Context, groupID int64) (*DuplicatePlan, error) {
	g, err := loadSource(ctx, s.db, groupID)
	if err != nil {
		return nil, err
	}
	from := s.baseYear(g)
	to := from + 1
	slug, err := pickSlug(ctx, s.db, nextSlugBase(g.slug, from, to))
	if err != nil {
		return nil, err
	}
	return &DuplicatePlan{Name: nextName(g.name, from, to), Slug: slug, Year: to}, nil
}

func shiftYear(v sql.NullString, layout string) any {
	if !v.Valid || v.String == "" {
		return nil
	}
	t, err := time.Parse(layout, v.String)
	if err != nil {
		return nil
	}
	return t.AddDate(1, 0, 0).Format(layout)
}

// Duplicate crée, dans une seule transaction, le groupe de l'année suivante :
// mêmes réglages (budget, consigne, options, plafond, langue, relances ; jour
// J et révélation décalés d'un an), mêmes participants (rôle dans le tirage
// compris), foyers, exclusions et associations imposées. Rien du tirage, des
// lettres, des accès personnels ni des inscriptions n'est copié. Le secret
// d'administration du nouveau groupe est renvoyé une seule fois.
func (s *Service) Duplicate(ctx context.Context, groupID int64) (*DuplicateResult, error) {
	secret, err := random.Token()
	if err != nil {
		return nil, errs.Internal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	g, err := loadSource(ctx, tx, groupID)
	if err != nil {
		return nil, err
	}
	from := s.baseYear(g)
	to := from + 1
	slug, err := pickSlug(ctx, tx, nextSlugBase(g.slug, from, to))
	if err != nil {
		return nil, err
	}
	name := nextName(g.name, from, to)
	now := s.clock.Now().UTC()
	var budget any
	if g.budget.Valid {
		budget = g.budget.Int64
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO groups
		(slug, name, budget_cents, budget_version, gift_instructions, event_date, reveal_at,
		 admin_can_view_pair, max_gifts_per_giver, admin_secret_hash, created_at)
		VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`,
		slug, name, budget, g.instructions, shiftYear(g.eventDate, groups.DateLayout),
		shiftYear(g.revealAt, time.RFC3339), g.canView, g.maxGifts, secretDigest(secret), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, errs.Internal(err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return nil, errs.Internal(err)
	}
	if err := copyGroupRows(ctx, tx, groupID, newID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, errs.Internal(err)
	}
	return &DuplicateResult{GroupID: newID, Name: name, Slug: slug, Year: to, AdminSecret: secret}, nil
}

func copyGroupRows(ctx context.Context, tx *sql.Tx, from, to int64, now time.Time) error {
	// Participants : identité, courriel, indice, rôle et langue ; ni lien ni
	// code d'accès.
	rows, err := tx.QueryContext(ctx, `SELECT id, name, first_name, name_key, email, gift_hint, draw_role, locale
		FROM participants WHERE group_id = ? ORDER BY joined_at, id`, from)
	if err != nil {
		return errs.Internal(err)
	}
	type person struct {
		id                                          int64
		name, first, key, email, hint, role, locale string
	}
	var people []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.id, &p.name, &p.first, &p.key, &p.email, &p.hint, &p.role, &p.locale); err != nil {
			rows.Close()
			return errs.Internal(err)
		}
		people = append(people, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return errs.Internal(err)
	}
	joined := now.Format(time.RFC3339Nano)
	ids := map[int64]int64{}
	for _, p := range people {
		res, err := tx.ExecContext(ctx, `INSERT INTO participants
			(group_id, name, first_name, name_key, email, gift_hint, draw_role, locale, joined_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, to, p.name, p.first, p.key, p.email, p.hint, p.role, p.locale, joined)
		if err != nil {
			return errs.Internal(err)
		}
		if ids[p.id], err = res.LastInsertId(); err != nil {
			return errs.Internal(err)
		}
	}

	// Foyers et membres.
	hrows, err := tx.QueryContext(ctx, `SELECT id, name FROM households WHERE group_id = ? ORDER BY id`, from)
	if err != nil {
		return errs.Internal(err)
	}
	type house struct {
		id   int64
		name string
	}
	var houses []house
	for hrows.Next() {
		var h house
		if err := hrows.Scan(&h.id, &h.name); err != nil {
			hrows.Close()
			return errs.Internal(err)
		}
		houses = append(houses, h)
	}
	hrows.Close()
	if err := hrows.Err(); err != nil {
		return errs.Internal(err)
	}
	for _, h := range houses {
		res, err := tx.ExecContext(ctx, `INSERT INTO households(group_id, name) VALUES (?, ?)`, to, h.name)
		if err != nil {
			return errs.Internal(err)
		}
		hid, err := res.LastInsertId()
		if err != nil {
			return errs.Internal(err)
		}
		if err := copyPairs(ctx, tx, `SELECT participant_id, 0 FROM household_members WHERE household_id = ?`, h.id,
			func(a, _ int64) error {
				na, ok := ids[a]
				if !ok {
					return nil // membre disparu : ignoré
				}
				_, err := tx.ExecContext(ctx, `INSERT INTO household_members(participant_id, household_id) VALUES (?, ?)`, na, hid)
				return err
			}); err != nil {
			return err
		}
	}

	// Exclusions manuelles et associations imposées.
	for _, table := range []string{"manual_exclusions", "forced_pairs"} {
		if err := copyPairs(ctx, tx, `SELECT giver_id, receiver_id FROM `+table+` WHERE group_id = ? ORDER BY id`, from,
			func(a, b int64) error {
				na, okA := ids[a]
				nb, okB := ids[b]
				if !okA || !okB {
					return nil
				}
				_, err := tx.ExecContext(ctx, `INSERT INTO `+table+`(group_id, giver_id, receiver_id) VALUES (?, ?, ?)`, to, na, nb)
				return err
			}); err != nil {
			return err
		}
	}

	// Réglages : langue par défaut et rythme des relances.
	if _, err := tx.ExecContext(ctx, `INSERT INTO group_settings(group_id, default_lang)
		SELECT ?, default_lang FROM group_settings WHERE group_id = ?`, to, from); err != nil {
		return errs.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reminder_group_rules(group_id, enabled, first_delay_days, spacing_days, max_per_type)
		SELECT ?, enabled, first_delay_days, spacing_days, max_per_type FROM reminder_group_rules WHERE group_id = ?`, to, from); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// copyPairs lit toutes les lignes (a, b) de la requête avant d'appeler fn
// sur chacune (pas d'écriture pendant la lecture).
func copyPairs(ctx context.Context, tx *sql.Tx, query string, arg int64, fn func(a, b int64) error) error {
	rows, err := tx.QueryContext(ctx, query, arg)
	if err != nil {
		return errs.Internal(err)
	}
	var pairs [][2]int64
	for rows.Next() {
		var p [2]int64
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			rows.Close()
			return errs.Internal(err)
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return errs.Internal(err)
	}
	for _, p := range pairs {
		if err := fn(p[0], p[1]); err != nil {
			return errs.Internal(err)
		}
	}
	return nil
}
