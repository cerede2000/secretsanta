// Package sources fournit les sources de suggestions de cadeaux : SearXNG
// auto-hébergé, Brave, Serper, puis un catalogue de secours embarqué.
// Les requêtes ne portent que des mots-clés de cadeau et la tranche de budget,
// jamais d'identité. Aucune clé n'apparaît dans les erreurs ni les journaux.
package sources

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// Band est une tranche de budget.
type Band string

// Tranches de budget.
const (
	BandAll     Band = "all" // toutes gammes (groupe sans budget)
	BandUnder20 Band = "under20"
	Band20to50  Band = "20to50"
	Band50to100 Band = "50to100"
	BandOver100 Band = "over100"
)

// Bands liste toutes les tranches.
var Bands = []Band{BandAll, BandUnder20, Band20to50, Band50to100, BandOver100}

// BandFor donne la tranche d'un budget en centimes (nil = sans budget).
func BandFor(cents *int64) Band {
	switch {
	case cents == nil || *cents <= 0:
		return BandAll
	case *cents < 2000:
		return BandUnder20
	case *cents < 5000:
		return Band20to50
	case *cents < 10000:
		return Band50to100
	default:
		return BandOver100
	}
}

// Query est la requête : mots-clés de cadeau et tranche, rien d'autre.
type Query struct {
	Keywords []string
	Band     Band
	Lang     string // "fr" ou "en"
	Limit    int    // 0 = 5
}

func (q Query) limit() int {
	if q.Limit <= 0 {
		return 5
	}
	return q.Limit
}

func (q Query) lang() string {
	if strings.HasPrefix(strings.ToLower(q.Lang), "en") {
		return "en"
	}
	return "fr"
}

// text compose la chaîne de recherche.
func (q Query) text() string {
	kw := strings.Join(q.Keywords, " ")
	suffix := map[Band]string{
		BandUnder20: "moins de 20 euros", Band20to50: "entre 20 et 50 euros",
		Band50to100: "entre 50 et 100 euros", BandOver100: "plus de 100 euros",
	}
	if q.lang() == "en" {
		suffix = map[Band]string{
			BandUnder20: "under 20 euros", Band20to50: "20 to 50 euros",
			Band50to100: "50 to 100 euros", BandOver100: "over 100 euros",
		}
	}
	base := "idée cadeau Noël"
	if q.lang() == "en" {
		base = "Christmas gift idea"
	}
	return strings.TrimSpace(base + " " + kw + " " + suffix[q.Band])
}

// Suggestion est une proposition de cadeau.
type Suggestion struct {
	Title       string
	URL         string
	Description string
	Image       string
	Price       string
	Source      string
}

// Source est un fournisseur de suggestions.
type Source interface {
	Name() string
	Search(ctx context.Context, q Query) ([]Suggestion, error)
}

// ErrNoResult : la source n'a rien renvoyé.
var ErrNoResult = errors.New("aucun résultat")

// Chain essaie les sources dans l'ordre ; une source en panne ou vide ne
// bloque pas les suivantes. Le catalogue embarqué ferme toujours la chaîne.
type Chain struct {
	sources []Source
	log     *slog.Logger
}

// NewChain construit la chaîne ; le catalogue de secours est ajouté en dernier.
func NewChain(log *slog.Logger, ordered ...Source) *Chain {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	list := make([]Source, 0, len(ordered)+1)
	for _, s := range ordered {
		if s != nil {
			list = append(list, s)
		}
	}
	list = append(list, NewCatalog())
	return &Chain{sources: list, log: log}
}

// Names donne l'ordre des sources.
func (c *Chain) Names() []string {
	out := make([]string, len(c.sources))
	for i, s := range c.sources {
		out[i] = s.Name()
	}
	return out
}

// Search renvoie les suggestions de la première source qui répond.
func (c *Chain) Search(ctx context.Context, q Query) ([]Suggestion, error) {
	if q.Band == "" {
		q.Band = BandAll
	}
	for _, s := range c.sources {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		res, err := s.Search(ctx, q)
		if err != nil || len(res) == 0 {
			c.log.Warn("source de suggestions indisponible", "source", s.Name(), "vide", err == nil)
			continue
		}
		if n := q.limit(); len(res) > n {
			res = res[:n]
		}
		return res, nil
	}
	return nil, ErrNoResult
}
