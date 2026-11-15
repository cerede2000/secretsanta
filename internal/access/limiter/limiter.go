// Package limiter fournit le limiteur de requêtes à seaux de l'accès
// participant (F2b), réutilisé par l'inscription par lien (F11) et par la
// connexion administrateur (F2e).
//
// Chaque seau compte les demandes d'une clé (groupe, nature, valeur) dans une
// fenêtre fixe qui s'ouvre à la première demande et dure Rule.Window. Une
// demande porte plusieurs clés (en général l'adresse IP et la cible) : elle
// n'est autorisée que si tous ses seaux ont encore de la place, et seule une
// demande autorisée est comptée. Les compteurs vivent en mémoire du processus.
package limiter

import (
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
)

// Rule fixe le nombre maximal de demandes par fenêtre.
type Rule struct {
	Limit  int
	Window time.Duration
}

var (
	// MagicLinkRequests : 5 demandes de lien magique par fenêtre de 15 minutes.
	MagicLinkRequests = Rule{Limit: 5, Window: 15 * time.Minute}
	// CodeAttempts : 10 tentatives de code court par fenêtre de 15 minutes.
	CodeAttempts = Rule{Limit: 10, Window: 15 * time.Minute}
)

// Natures de clé.
const (
	KindIP     = "ip"
	KindTarget = "cible"
)

// Key désigne un seau. Group isole les groupes : deux groupes ne partagent
// jamais un compteur (0 désigne l'instance, hors de tout groupe).
type Key struct {
	Group int64
	Kind  string
	Value string
}

// IPKey renvoie la clé d'une adresse IP dans un groupe. Les adresses sont
// normalisées : IPv4 mappée en IPv6 ramenée à IPv4, IPv6 ramenée à son
// préfixe /64. Une valeur illisible est gardée telle quelle.
func IPKey(group int64, ip string) Key {
	return Key{Group: group, Kind: KindIP, Value: normaliseIP(ip)}
}

// TargetKey renvoie la clé d'une cible (adresse de courriel, compte, code…)
// dans un groupe. La valeur est débarrassée des espaces de bord et mise en
// minuscules : « Anne@Example.org » et « anne@example.org » partagent un seau.
func TargetKey(group int64, target string) Key {
	return Key{Group: group, Kind: KindTarget, Value: strings.ToLower(strings.TrimSpace(target))}
}

func normaliseIP(s string) string {
	s = strings.TrimSpace(s)
	a, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	a = a.Unmap().WithZone("")
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return a.String()
	}
	return p.String()
}

// Decision est le verdict d'une demande.
type Decision struct {
	Allowed bool
	// RetryAfter est, en cas de refus, le temps avant que tous les seaux
	// pleins rouvrent une fenêtre.
	RetryAfter time.Duration
}

// ErrLimited signale une demande refusée par le limiteur.
var ErrLimited = errors.New("limiter: trop de demandes")

// Error renvoie l'erreur typée à présenter quand la demande est refusée.
// Elle enveloppe ErrLimited (errors.Is) pour qu'une couche HTTP réponde 429.
func Error() *errs.Error {
	return errs.Wrap(errs.KindForbidden, "Trop de demandes. Réessaie dans quelques minutes.", ErrLimited)
}

type bucket struct {
	start time.Time
	count int
}

// Limiter est un limiteur à seaux, sûr en concurrence.
type Limiter struct {
	clk  clock.Clock
	rule Rule

	mu        sync.Mutex
	buckets   map[Key]*bucket
	lastSweep time.Time
}

// New crée un limiteur pour la règle donnée. Une règle sans limite positive ou
// sans fenêtre positive est une erreur de programmation et provoque un panic.
func New(clk clock.Clock, rule Rule) *Limiter {
	if clk == nil || rule.Limit <= 0 || rule.Window <= 0 {
		panic("limiter: horloge ou règle invalide")
	}
	return &Limiter{clk: clk, rule: rule, buckets: map[Key]*bucket{}, lastSweep: clk.Now()}
}

// Rule renvoie la règle du limiteur.
func (l *Limiter) Rule() Rule { return l.rule }

// Allow décide d'une demande portant les clés données et la compte dans
// chacun de ses seaux si elle est autorisée. Une demande sans clé est
// refusée.
func (l *Limiter) Allow(keys ...Key) Decision {
	if len(keys) == 0 {
		return Decision{Allowed: false, RetryAfter: l.rule.Window}
	}
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)

	var wait time.Duration
	for _, k := range keys {
		b := l.buckets[k]
		if b == nil || !now.Before(b.start.Add(l.rule.Window)) {
			continue
		}
		if b.count >= l.rule.Limit {
			if w := b.start.Add(l.rule.Window).Sub(now); w > wait {
				wait = w
			}
		}
	}
	if wait > 0 {
		return Decision{Allowed: false, RetryAfter: wait}
	}
	seen := make(map[Key]struct{}, len(keys))
	for _, k := range keys {
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		b := l.buckets[k]
		if b == nil || !now.Before(b.start.Add(l.rule.Window)) {
			l.buckets[k] = &bucket{start: now, count: 1}
			continue
		}
		b.count++
	}
	return Decision{Allowed: true}
}

// Peek décide d'une demande sans rien compter : le refus est celui qu'Allow
// prononcerait, mais une demande autorisée ne consomme aucune place. Une
// demande sans clé est refusée.
func (l *Limiter) Peek(keys ...Key) Decision {
	if len(keys) == 0 {
		return Decision{Allowed: false, RetryAfter: l.rule.Window}
	}
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	var wait time.Duration
	for _, k := range keys {
		b := l.buckets[k]
		if b == nil || !now.Before(b.start.Add(l.rule.Window)) {
			continue
		}
		if b.count >= l.rule.Limit {
			if w := b.start.Add(l.rule.Window).Sub(now); w > wait {
				wait = w
			}
		}
	}
	if wait > 0 {
		return Decision{Allowed: false, RetryAfter: wait}
	}
	return Decision{Allowed: true}
}

// Hit compte une demande dans chacun des seaux donnés sans rien refuser
// (un seau déjà plein reste plein). Associé à Peek, il permet de ne compter
// que les échecs : Peek avant l'épreuve, Hit si elle échoue.
func (l *Limiter) Hit(keys ...Key) {
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	seen := make(map[Key]struct{}, len(keys))
	for _, k := range keys {
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		b := l.buckets[k]
		if b == nil || !now.Before(b.start.Add(l.rule.Window)) {
			l.buckets[k] = &bucket{start: now, count: 1}
			continue
		}
		b.count++
	}
}

// Len renvoie le nombre de seaux en mémoire.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// sweep retire les seaux dont la fenêtre est close, au plus une fois par
// fenêtre. Appelé verrou pris.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.rule.Window {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if !now.Before(b.start.Add(l.rule.Window)) {
			delete(l.buckets, k)
		}
	}
}
