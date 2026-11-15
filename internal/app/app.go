// Package app assemble le serveur : migrations, chaîne de middlewares, routes,
// tâches périodiques et arrêt propre. Fichier d'intégration.
//
// Point d'extension : chaque fonctionnalité fournit une fonction
// `Register(a *App) error` (dans son propre fichier de ce paquet) qui ajoute
// ses routes à a.Mux, ses tâches via a.Every et ses arrêts via a.OnClose. Pour
// brancher un module, ajouter UNE ligne à la liste `features` (features.go) et
// UNE ligne à `migrationSources` (migrations.go).
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"secretsanta/internal/access/token"
	"secretsanta/internal/config"
	"secretsanta/internal/groups"
	"secretsanta/internal/health"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/letter"
	"secretsanta/internal/maildkim"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/participants"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/scheduler"
	"secretsanta/internal/ssrf"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// purgeInterval est la période des purges (sessions, liens expirés).
const purgeInterval = 15 * time.Minute

// App est le serveur assemblé.
type App struct {
	Cfg   config.Config
	DB    *sql.DB
	Clock clock.Clock
	// Cat est le catalogue de textes (embarqué + dossier externe).
	Cat      *catalog.Catalog
	Version  string
	Policy   websec.Policy
	Sessions *websec.Sessions
	Groups   *groups.Service
	// Services des vagues intégrées (aucune route métier encore).
	Participants *participants.Service
	Rules        *rules.Service
	Tokens       *token.Service
	// Bus est le bus d'événements (en mémoire, synchrone).
	Bus ports.EventBus
	// SSRF est la garde de récupération externe (aucune route de recherche encore).
	SSRF *ssrf.Guard
	// Images sert les copies locales d'images sous /images/.
	Images *ssrf.Store
	// Letter est le service des lettres (aucune route encore).
	Letter *letter.Service
	// Mail est nil tant qu'aucune adresse d'expédition n'est configurée.
	Mail *mailmsg.Builder
	// DKIM est toujours non nil ; Enabled() dit s'il signe.
	DKIM *maildkim.Signer
	// Mux reçoit les routes des fonctionnalités (derrière la chaîne de middlewares).
	Mux *http.ServeMux

	handler http.Handler
	sched   *scheduler.Scheduler
	mu      sync.Mutex
	closers []func()
}

// New migre la base, construit les services et la chaîne de middlewares.
func New(cfg config.Config, database *sql.DB, clk clock.Clock, version string) (*App, error) {
	if err := Migrate(database); err != nil {
		return nil, fmt.Errorf("migrations : %w", err)
	}
	if cfg.DefaultLang == "" {
		cfg.DefaultLang = config.DefaultLang
	}
	cat, err := catalog.Load(web.I18n, catalog.Options{
		DefaultLang: cfg.DefaultLang, ExternalDir: cfg.I18nDir, Logger: slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("textes : %w", err)
	}
	policy, err := websec.NewPolicy(cfg.PublicURL)
	if err != nil {
		return nil, err
	}
	a := &App{
		Cfg: cfg, DB: database, Clock: clk, Cat: cat, Version: version,
		Policy: policy, Mux: http.NewServeMux(), sched: scheduler.New(clk),
	}
	a.Sessions = websec.NewSessions(database, clk, policy, websec.SessionOptions{})
	a.Every("websec.purge_sessions", purgeInterval, func(ctx context.Context) error {
		_, err := a.Sessions.PurgeExpired(ctx)
		return err
	})
	a.Mux.Handle("/health", health.Handler(database, version))

	for _, f := range features {
		if err := f(a); err != nil {
			return nil, err
		}
	}

	csrf := websec.NewCSRF(policy, websec.CSRFOptions{
		OnReject: func(w http.ResponseWriter, r *http.Request, _ error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(cat.T(negotiate.Lang(r.Context(), cfg.DefaultLang), "core.csrf.reject", nil) + "\n"))
		},
	})
	// Ordre : en-têtes, CSRF, sessions, négociation de langue, mux. Les ports
	// GroupLanguage (nil) et ParticipantLocale (nil) sont des ancres : les
	// pages de groupe et de participant les fourniront.
	lang := negotiate.Middleware(cat, cfg.DefaultLang, nil)
	a.handler = websec.SecurityHeaders(policy)(csrf.Middleware(a.Sessions.Middleware(lang(a.Mux))))
	return a, nil
}

// Handler renvoie le gestionnaire HTTP complet.
func (a *App) Handler() http.Handler { return a.handler }

// OnClose enregistre un arrêt, appelé par Close dans l'ordre inverse.
func (a *App) OnClose(f func()) {
	a.mu.Lock()
	a.closers = append(a.closers, f)
	a.mu.Unlock()
}

// Every planifie une tâche périodique (première exécution après une période).
// Une erreur est journalisée et la tâche reste planifiée.
func (a *App) Every(name string, every time.Duration, task scheduler.Task) {
	var run scheduler.Task
	run = func(ctx context.Context) error {
		err := task(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("tâche périodique en échec", "tâche", name, "erreur", err)
		}
		a.sched.At(a.Clock.Now().Add(every), run)
		return nil
	}
	a.sched.At(a.Clock.Now().Add(every), run)
}

// Start lance l'ordonnanceur ; il s'arrête avec ctx.
func (a *App) Start(ctx context.Context) {
	go a.sched.Run(ctx, time.Minute)
}

// Close exécute les arrêts enregistrés (attente des envois en cours, etc.).
func (a *App) Close() {
	a.mu.Lock()
	cs := a.closers
	a.closers = nil
	a.mu.Unlock()
	for i := len(cs) - 1; i >= 0; i-- {
		cs[i]()
	}
}
