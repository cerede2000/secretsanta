// Package negotiate choisit la langue d'une requête : cookie lang, puis
// en-tête Accept-Language (valeurs de qualité), puis langue par défaut du
// groupe, puis langue par défaut de l'instance (APP_DEFAULT_LANG). Une langue
// non chargée passe à la règle suivante.
package negotiate

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// CookieName est le nom du cookie de langue. Il ne porte aucune donnée
// personnelle : seulement un code de langue.
const CookieName = "lang"

// Matcher est ce que la négociation demande au catalogue de textes
// (satisfait par *catalog.Catalog) : renvoie la langue chargée qui correspond
// à un code.
type Matcher interface {
	Match(tag string) (string, bool)
}

// GroupLanguage est le port de la langue par défaut d'un groupe (fourni par
// F2e). slug est le premier segment du chemin de la requête ; ok est faux si
// le groupe est inconnu ou n'a pas de langue par défaut.
type GroupLanguage interface {
	GroupLanguage(ctx context.Context, slug string) (lang string, ok bool)
}

// ParticipantLocale est le port d'enregistrement de la langue du participant
// connecté (fourni par F3). La mise en œuvre retrouve le participant par la
// session de la requête ; elle renvoie recorded=false s'il n'y en a pas.
type ParticipantLocale interface {
	RememberLocale(ctx context.Context, r *http.Request, lang string) (recorded bool, err error)
}

// Input regroupe les sources de langue, de la plus à la moins prioritaire.
type Input struct {
	Cookie         string
	AcceptLanguage string
	GroupLang      string
	InstanceLang   string
}

// Choose applique les règles et renvoie toujours une langue : celle de
// l'instance si aucune règle ne donne de langue chargée (même non chargée,
// pour ne jamais renvoyer une chaîne vide).
func Choose(m Matcher, in Input) string {
	if l, ok := match(m, in.Cookie); ok {
		return l
	}
	for _, tag := range ParseAcceptLanguage(in.AcceptLanguage) {
		if l, ok := match(m, tag); ok {
			return l
		}
	}
	if l, ok := match(m, in.GroupLang); ok {
		return l
	}
	if l, ok := match(m, in.InstanceLang); ok {
		return l
	}
	return in.InstanceLang
}

func match(m Matcher, tag string) (string, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" || tag == "*" || len(tag) > 35 {
		return "", false
	}
	return m.Match(tag)
}

// ParseAcceptLanguage renvoie les étiquettes par qualité décroissante (ordre
// de l'en-tête à qualité égale) ; q=0 exclut l'étiquette.
func ParseAcceptLanguage(h string) []string {
	type item struct {
		tag string
		q   float64
		pos int
	}
	var items []item
	for i, part := range strings.Split(h, ",") {
		fields := strings.Split(part, ";")
		tag := strings.TrimSpace(fields[0])
		if tag == "" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if v, ok := strings.CutPrefix(strings.ToLower(f), "q="); ok {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && parsed >= 0 && parsed <= 1 {
					q = parsed
				} else {
					q = 0
				}
			}
		}
		if q > 0 {
			items = append(items, item{tag, q, i})
		}
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].q > items[b].q })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.tag
	}
	return out
}

type ctxKey struct{}

// WithLang place la langue dans le contexte.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// Lang renvoie la langue du contexte ; fallback si le middleware n'est pas passé.
func Lang(ctx context.Context, fallback string) string {
	if l, ok := ctx.Value(ctxKey{}).(string); ok && l != "" {
		return l
	}
	return fallback
}

// Middleware pose la langue négociée dans le contexte de chaque requête.
// group peut être nil.
func Middleware(m Matcher, instanceLang string, group GroupLanguage) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			in := Input{AcceptLanguage: r.Header.Get("Accept-Language"), InstanceLang: instanceLang}
			if c, err := r.Cookie(CookieName); err == nil {
				in.Cookie = c.Value
			}
			// La langue du groupe ne sert qu'en dernier recours : on ne
			// l'interroge que si ni cookie ni navigateur ne tranchent.
			if group != nil && !decided(m, in) {
				if slug := firstSegment(r.URL.Path); slug != "" {
					if l, ok := group.GroupLanguage(r.Context(), slug); ok {
						in.GroupLang = l
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(WithLang(r.Context(), Choose(m, in))))
		})
	}
}

func decided(m Matcher, in Input) bool {
	if _, ok := match(m, in.Cookie); ok {
		return true
	}
	for _, tag := range ParseAcceptLanguage(in.AcceptLanguage) {
		if _, ok := match(m, tag); ok {
			return true
		}
	}
	return false
}

func firstSegment(p string) string {
	p = strings.TrimPrefix(p, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	return p
}
