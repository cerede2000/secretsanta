// Package suggestions propose des idées de cadeau au tireur (F8c.1) : budget
// du groupe quand il existe (sinon toutes gammes et filtre de prix libre),
// thème de Noël, mots-clés tirés de la lettre du tiré sans nom ni courriel,
// cache par cible et par budget, quotas, accès réservé au tireur de la cible.
package suggestions

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/enrich"
	"secretsanta/internal/groups"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/sources"
	"secretsanta/internal/ssrf"
)

// Module est le nom du module dans schema_migrations (après groups et participants).
const Module = "suggestions"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par l'orchestrateur.
func Migrations() ([]db.Migration, error) { return db.LoadFS(migrationsFS, "migrations") }

// Messages français.
const (
	msgNoPage    = "Tu n'as pas de page de suggestions."
	msgNotOpen   = "Les suggestions ne sont pas encore disponibles."
	msgNoGift    = "Ce cadeau n'existe pas."
	msgPrice     = "Le filtre de prix n'est pas valide."
	msgNoSources = "Aucune source de suggestions n'est disponible pour le moment."
)

// ErrNoSource : aucune source n'a répondu et rien n'est en cache.
var ErrNoSource = errors.New("suggestions : aucune source disponible")

// Réglages par défaut.
const (
	DefaultTTL        = 24 * time.Hour
	DefaultQuotaWin   = time.Hour
	DefaultGiverQuota = 6
	DefaultGroupQuota = 60
	searchLimit       = 10
	maxItems          = 8
)

// Dépendances, sous forme d'interfaces pour les tests.
type (
	// Targets : *draw.Service.
	Targets interface {
		MyTargets(ctx context.Context, a participants.Actor, groupID int64) (*draw.MyTargets, error)
	}
	// Groups : *groups.Service.
	Groups interface {
		ByID(ctx context.Context, id int64) (*groups.Group, error)
	}
	// Letters : *letter.Service.
	Letters interface {
		Get(ctx context.Context, groupID, participantID int64) (*letter.Letter, error)
	}
	// People : *participants.Service.
	People interface {
		List(ctx context.Context, a participants.Actor, groupID int64) ([]participants.Participant, error)
	}
	// Searcher : *sources.Chain.
	Searcher interface {
		Search(ctx context.Context, q sources.Query) ([]sources.Suggestion, error)
	}
	// Images copie une image distante dans le volume et renvoie son chemin local.
	Images interface {
		Copy(ctx context.Context, rawURL string) (string, error)
	}
)

// GuardImages adapte ssrf.Store et ssrf.Guard : requêtes sans Referer ni cookie.
func GuardImages(store *ssrf.Store, g *ssrf.Guard) Images { return guardImages{store, g} }

type guardImages struct {
	store *ssrf.Store
	guard *ssrf.Guard
}

func (a guardImages) Copy(ctx context.Context, raw string) (string, error) {
	li, err := a.store.Copy(ctx, a.guard, raw)
	if err != nil {
		return "", err
	}
	return li.Src, nil
}

// Options règle le cache et les quotas (zéro = défaut).
type Options struct {
	TTL        time.Duration
	QuotaWin   time.Duration
	GiverQuota int // recherches réelles par tireur et par fenêtre
	GroupQuota int // recherches réelles par groupe et par fenêtre
	Logger     *slog.Logger
}

// Service produit les suggestions.
type Service struct {
	db      *sql.DB
	clock   clock.Clock
	targets Targets
	groups  Groups
	letters Letters
	people  People
	search  Searcher
	images  Images
	opt     Options
}

// NewService construit le service.
func NewService(d *sql.DB, c clock.Clock, t Targets, g Groups, l Letters, p People, s Searcher, img Images, o Options) *Service {
	if o.TTL <= 0 {
		o.TTL = DefaultTTL
	}
	if o.QuotaWin <= 0 {
		o.QuotaWin = DefaultQuotaWin
	}
	if o.GiverQuota <= 0 {
		o.GiverQuota = DefaultGiverQuota
	}
	if o.GroupQuota <= 0 {
		o.GroupQuota = DefaultGroupQuota
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: d, clock: c, targets: t, groups: g, letters: l, people: p, search: s, images: img, opt: o}
}

// Item est une proposition. ImageSrc est un chemin local (jamais externe) ;
// PriceCents vaut 0 quand le prix est inconnu.
type Item struct {
	Title      string `json:"title"`
	URL        string `json:"url,omitempty"`
	ImageSrc   string `json:"image,omitempty"`
	PriceCents int64  `json:"price,omitempty"`
	Source     string `json:"source,omitempty"`
}

// Request demande les suggestions d'une cible.
type Request struct {
	Actor   participants.Actor
	GroupID int64
	// Gift est le numéro du cadeau (1 ou 2) ; 0 vaut 1.
	Gift int
	// MinCents et MaxCents : filtre de prix libre, facultatif.
	MinCents, MaxCents *int64
	// Refresh ignore le cache (compte dans le quota).
	Refresh bool
	Lang    string
}

// Result est la réponse. Seul le tireur de la cible la reçoit.
type Result struct {
	Items []Item
	// BudgetCents : budget du groupe appliqué (nil = sans budget).
	BudgetCents *int64
	// FromLetter : les mots-clés viennent de la lettre du tiré.
	FromLetter bool
	// Cached : la réponse vient du cache ; Throttled : quota atteint, cache servi.
	Cached, Throttled bool
	// Unavailable : aucune source disponible et rien en cache.
	Unavailable bool
}

// Suggest renvoie les suggestions de la cible du tireur.
func (s *Service) Suggest(ctx context.Context, r Request) (*Result, error) {
	receiver, err := s.target(ctx, r)
	if err != nil {
		return nil, err
	}
	if (r.MinCents != nil && *r.MinCents < 0) || (r.MaxCents != nil && *r.MaxCents < 0) ||
		(r.MinCents != nil && r.MaxCents != nil && *r.MinCents > *r.MaxCents) {
		return nil, errs.Validation(msgPrice)
	}
	g, err := s.groups.ByID(ctx, r.GroupID)
	if err != nil {
		return nil, err
	}
	lang := "fr"
	if strings.HasPrefix(strings.ToLower(r.Lang), "en") {
		lang = "en"
	}
	kw, fromLetter, err := s.keywords(ctx, r, receiver)
	if err != nil {
		return nil, err
	}
	key := cacheKey{group: r.GroupID, giver: r.Actor.ParticipantID, receiver: receiver,
		budget: budgetKey(g), keywords: keywordsKey(kw), lang: lang}
	res := &Result{BudgetCents: g.BudgetCents, FromLetter: fromLetter}

	items, fresh, found, err := s.cacheGet(ctx, key)
	if err != nil {
		return nil, err
	}
	switch {
	case found && fresh && !r.Refresh:
		res.Cached = true
	default:
		ok, err := s.quotaOK(ctx, r.GroupID, r.Actor.ParticipantID)
		if err != nil {
			return nil, err
		}
		if !ok {
			if !found {
				return nil, limiter.Error()
			}
			res.Cached, res.Throttled = true, true
			break
		}
		live, err := s.live(ctx, g, r.Actor.ParticipantID, kw, lang)
		if errors.Is(err, sources.ErrNoResult) || errors.Is(err, ErrNoSource) {
			if found {
				res.Cached = true
				break
			}
			res.Unavailable = true
			return res, nil
		}
		if err != nil {
			return nil, errs.Internal(err)
		}
		items = live
		if err := s.cachePut(ctx, key, items); err != nil {
			return nil, err
		}
	}
	res.Items = filter(items, g.BudgetCents, r.MinCents, r.MaxCents)
	return res, nil
}

// target vérifie l'accès : seul le tireur de la cible, jamais une personne
// sans cible (réception seule), jamais avant la révélation.
func (s *Service) target(ctx context.Context, r Request) (int64, error) {
	if r.Actor.Kind != participants.ActorParticipant || r.Actor.GroupID != r.GroupID || r.GroupID == 0 {
		return 0, errs.Forbidden(msgNoPage)
	}
	mt, err := s.targets.MyTargets(ctx, r.Actor, r.GroupID)
	if err != nil {
		return 0, err
	}
	if mt == nil || !mt.Done || !mt.Ready {
		return 0, errs.Conflict(msgNotOpen)
	}
	if len(mt.Targets) == 0 {
		return 0, errs.Forbidden(msgNoPage)
	}
	gift := r.Gift
	if gift == 0 {
		gift = 1
	}
	if gift < 1 || gift > len(mt.Targets) {
		return 0, errs.NotFound(msgNoGift)
	}
	return mt.Targets[gift-1].ReceiverID, nil
}

// keywords lit la lettre du tiré, uniquement pour son tireur.
func (s *Service) keywords(ctx context.Context, r Request, receiver int64) ([]string, bool, error) {
	l, err := s.letters.Get(ctx, r.GroupID, receiver)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if l == nil || len(l.Ideas) == 0 {
		return nil, false, nil
	}
	people, err := s.people.List(ctx, r.Actor, r.GroupID)
	if err != nil {
		return nil, false, err
	}
	var names []string
	for _, p := range people {
		names = append(names, p.Name, p.FirstName)
		if p.Email != "" {
			names = append(names, strings.NewReplacer("@", " ", ".", " ", "_", " ", "-", " ").Replace(p.Email))
		}
	}
	var texts []string
	for _, i := range l.Ideas {
		texts = append(texts, i.Title, i.Text)
	}
	kw := Keywords(texts, names)
	return kw, len(kw) > 0, nil
}

// live interroge les sources (mots-clés de cadeau et tranche seulement) et
// copie les images en local.
func (s *Service) live(ctx context.Context, g *groups.Group, giver int64, kw []string, lang string) ([]Item, error) {
	if s.search == nil {
		return nil, ErrNoSource
	}
	if err := s.recordSearch(ctx, g.ID, giver); err != nil {
		return nil, err
	}
	res, err := s.search.Search(ctx, sources.Query{Keywords: kw, Band: sources.BandFor(g.BudgetCents), Lang: lang, Limit: searchLimit})
	if err != nil {
		return nil, err
	}
	var out []Item
	seen := map[string]bool{}
	for _, r := range res {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			continue
		}
		link := ""
		if u, err := url.Parse(r.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			link = r.URL
		}
		dk := link
		if dk == "" {
			dk = "t:" + strings.ToLower(title)
		}
		if seen[dk] {
			continue
		}
		seen[dk] = true
		it := Item{Title: title, URL: link, Source: r.Source}
		if c, ok := enrich.ParsePrice(r.Price); ok {
			it.PriceCents = int64(c)
		}
		if r.Image != "" && s.images != nil {
			if src, err := s.images.Copy(ctx, r.Image); err == nil {
				it.ImageSrc = src
			} else {
				s.opt.Logger.Warn("copie d'image de suggestion refusée", "erreur", err)
			}
		}
		out = append(out, it)
		if len(out) == maxItems {
			break
		}
	}
	if len(out) == 0 {
		return nil, sources.ErrNoResult
	}
	return out, nil
}

// filter applique le budget (jamais de prix connu au-dessus) puis le filtre
// libre. Un prix inconnu n'est pas écarté : on ne peut pas le juger.
func filter(items []Item, budget, min, max *int64) []Item {
	var out []Item
	for _, it := range items {
		if it.PriceCents > 0 {
			if budget != nil && it.PriceCents > *budget {
				continue
			}
			if min != nil && it.PriceCents < *min {
				continue
			}
			if max != nil && it.PriceCents > *max {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

func budgetKey(g *groups.Group) string {
	if g.BudgetCents == nil {
		return "none"
	}
	return "c" + strconv.FormatInt(*g.BudgetCents, 10) + "v" + strconv.Itoa(g.BudgetVersion)
}

type cacheKey struct {
	group, giver, receiver int64
	budget, keywords, lang string
}

func (s *Service) cacheGet(ctx context.Context, k cacheKey) (items []Item, fresh, found bool, err error) {
	var raw, at string
	err = s.db.QueryRowContext(ctx, `SELECT items, fetched_at FROM suggestion_cache
		WHERE group_id = ? AND giver_id = ? AND receiver_id = ? AND budget_key = ? AND keywords_key = ? AND lang = ?`,
		k.group, k.giver, k.receiver, k.budget, k.keywords, k.lang).Scan(&raw, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, errs.Internal(err)
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, false, false, nil // entrée illisible : comme absente
	}
	t, _ := time.Parse(time.RFC3339Nano, at)
	return items, s.clock.Now().Sub(t) < s.opt.TTL, true, nil
}

func (s *Service) cachePut(ctx context.Context, k cacheKey, items []Item) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return errs.Internal(err)
	}
	now := s.clock.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO suggestion_cache
		(group_id, giver_id, receiver_id, budget_key, keywords_key, lang, items, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (group_id, giver_id, receiver_id, budget_key, keywords_key, lang)
		DO UPDATE SET items = excluded.items, fetched_at = excluded.fetched_at`,
		k.group, k.giver, k.receiver, k.budget, k.keywords, k.lang, string(raw), now.Format(time.RFC3339Nano))
	if err != nil {
		return errs.Internal(err)
	}
	// Ménage : les entrées de plus de sept TTL ne servent plus, même en repli.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM suggestion_cache WHERE fetched_at < ?`,
		now.Add(-7*s.opt.TTL).Format(time.RFC3339Nano))
	return nil
}

func (s *Service) quotaOK(ctx context.Context, groupID, giverID int64) (bool, error) {
	since := s.clock.Now().UTC().Add(-s.opt.QuotaWin).Format(time.RFC3339Nano)
	var perGiver, perGroup int
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(giver_id = ?), 0), COUNT(*) FROM suggestion_searches WHERE group_id = ? AND at > ?`,
		giverID, groupID, since).Scan(&perGiver, &perGroup)
	if err != nil {
		return false, errs.Internal(err)
	}
	return perGiver < s.opt.GiverQuota && perGroup < s.opt.GroupQuota, nil
}

func (s *Service) recordSearch(ctx context.Context, groupID int64, id int64) error {
	now := s.clock.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO suggestion_searches (group_id, giver_id, at) VALUES (?, ?, ?)`,
		groupID, id, now.Format(time.RFC3339Nano)); err != nil {
		return errs.Internal(err)
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM suggestion_searches WHERE at < ?`,
		now.Add(-24*time.Hour).Format(time.RFC3339Nano))
	return nil
}
