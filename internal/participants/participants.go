// Package participants porte les personnes d'un groupe (F3) : saisie nettoyée,
// noms uniques sans casse ni accents, rôle dans le tirage, suppression
// confirmée et édition après tirage.
package participants

import (
	"context"
	"database/sql"
	"embed"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"secretsanta/internal/db"
	"secretsanta/internal/shared/errs"
)

// Module est le nom du module dans schema_migrations.
const Module = "participants"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par l'orchestrateur.
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Bornes des saisies (en caractères).
const (
	NameMaxLen      = 80
	FirstNameMaxLen = 80
	GiftHintMaxLen  = 500
	EmailMaxLen     = 254
	LocaleMaxLen    = 35
	// MinParticipants est le minimum requis pour tirer (appliqué par F4).
	MinParticipants = 2
)

// DrawRole est le rôle d'un participant dans le tirage (valeurs exclusives).
type DrawRole string

const (
	RoleNormal         DrawRole = "normal"
	RoleReceptionSeule DrawRole = "reception_seule"
	RoleOffrePlus      DrawRole = "offre_plus"
	RoleUnSeul         DrawRole = "un_seul"
)

// Valid indique si r est l'une des quatre valeurs possibles.
func (r DrawRole) Valid() bool {
	switch r {
	case RoleNormal, RoleReceptionSeule, RoleOffrePlus, RoleUnSeul:
		return true
	}
	return false
}

// Participant est un participant tel que stocké. Les condensés du lien et du
// code ne sont jamais exposés.
type Participant struct {
	ID        int64
	GroupID   int64
	Name      string
	FirstName string
	Email     string
	GiftHint  string
	Role      DrawRole
	Locale    string
	JoinedAt  time.Time
}

// PublicView est la vue publique : ni courriel, ni indice, ni langue.
type PublicView struct {
	ID        int64
	FirstName string
	Name      string
}

// Public renvoie la vue publique du participant.
func (p Participant) Public() PublicView {
	return PublicView{ID: p.ID, FirstName: p.FirstName, Name: p.Name}
}

var foldMarks = runes.Remove(runes.In(unicode.Mn))

// NormalizeName renvoie la clé d'unicité d'un nom complet : sans accents,
// sans casse, espaces réduits. C'est l'unique règle d'unicité du paquet.
func NormalizeName(firstName, name string) string {
	full := strings.Join(strings.Fields(firstName+" "+name), " ")
	s, _, err := transform.String(transform.Chain(norm.NFD, foldMarks, norm.NFC), full)
	if err != nil {
		s = full
	}
	s = strings.ToLower(s)
	s = strings.NewReplacer("ß", "ss", "œ", "oe", "æ", "ae", "ø", "o", "ł", "l", "đ", "d").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// Querier est satisfait par *sql.DB et *sql.Tx.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NameExists indique si un participant du groupe porte déjà ce nom (même
// règle que l'ajout manuel). excludeID écarte un participant (édition) ; 0
// n'en écarte aucun.
func NameExists(ctx context.Context, q Querier, groupID int64, firstName, name string, excludeID int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM participants WHERE group_id = ? AND name_key = ? AND id <> ?`,
		groupID, NormalizeName(firstName, name), excludeID).Scan(&n)
	if err != nil {
		return false, errs.Internal(err)
	}
	return n > 0, nil
}

var localeRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// ValidLocale vérifie la syntaxe d'un code de langue (la liste des langues
// chargées est vérifiée par F13).
func ValidLocale(s string) bool { return len(s) <= LocaleMaxLen && localeRe.MatchString(s) }
