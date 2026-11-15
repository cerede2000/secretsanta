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

	"secretsanta/internal/access/magic"
	"secretsanta/internal/access/token"
	"secretsanta/internal/accesspages"
	"secretsanta/internal/admin/audit"
	"secretsanta/internal/admin/dashboard"
	"secretsanta/internal/config"
	"secretsanta/internal/draw"
	"secretsanta/internal/enrich"
	"secretsanta/internal/enrolment"
	"secretsanta/internal/grouppages"
	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/health"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/negotiate"
	"secretsanta/internal/letter"
	"secretsanta/internal/letterread"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildiag"
	"secretsanta/internal/maildkim"
	"secretsanta/internal/mailmsg"
	"secretsanta/internal/mailsuppress"
	"secretsanta/internal/mailunsub"
	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/sqlstore"
	"secretsanta/internal/registration/verify"
	"secretsanta/internal/reminderrules"
	"secretsanta/internal/reveal"
	"secretsanta/internal/rulechange"
	"secretsanta/internal/rules"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/scheduler"
	"secretsanta/internal/sources"
	"secretsanta/internal/ssrf"
	"secretsanta/internal/targetpage"
	"secretsanta/internal/websec"
	"secretsanta/web"
)

// purgeInterval est la période des purges (sessions, liens expirés).
const purgeInterval = 15 * time.Minute

// mailTickEvery est la période de passage de la file d'envoi.
const mailTickEvery = 5 * time.Second

// App est le serveur assemblé.
type App struct {
	// clientIP retrouve l'adresse réelle du client (TRUSTED_PROXIES,
	// CLIENT_IP_HEADER).
	clientIP func(*http.Request) string
	Cfg      config.Config
	DB       *sql.DB
	Clock    clock.Clock
	// Cat est le catalogue de textes (embarqué + dossier externe).
	Cat      *catalog.Catalog
	Version  string
	Policy   websec.Policy
	Sessions *websec.Sessions
	Groups   *groups.Service
	// Services des vagues intégrées.
	Participants *participants.Service
	Rules        *rules.Service
	Tokens       *token.Service
	// GroupSettings porte les réglages d'un groupe (langue par défaut).
	GroupSettings *groupsettings.Service
	// DrawState est le port DrawState réel (construit avant participants) ;
	// Draw est le service du tirage (aucune route encore).
	DrawState *draw.State
	Draw      *draw.Service
	// Enrolment crée les participants vérifiés (aucune route encore).
	Enrolment *enrolment.Service
	// RuleChange modifie règles et rôles (aucune route encore).
	RuleChange *rulechange.Service
	// Magic porte le lien magique de connexion.
	Magic *magic.Service
	// Unsub porte les préférences de désinscription (routes
	// /desinscription/{jeton} montées sur Mux).
	Unsub *mailunsub.Service
	// Reveal fait vivre la révélation : porte, date, ouverture planifiée, avis
	// « tirage prêt ». Drawing enchaîne tirage et révélation : un tirage, un
	// nouveau tirage ou un ajustement passent par lui pour que la révélation
	// suive.
	Reveal  *reveal.Service
	Drawing *Drawing
	// Sources et Enrich sont les services de suggestions (aucune page encore) ;
	// leurs requêtes sortantes passent par la garde SSRF.
	Sources *sources.Chain
	Enrich  *enrich.Amazon
	// MailQueue est la file d'envoi ; nil sans adresse d'expédition. Elle ne
	// tourne que si un transport est fourni (voir mailTransport).
	MailQueue *queue.Service
	// Suppress est la liste des adresses refusées définitivement (consultée
	// par la file d'envoi) ; MailDiag le diagnostic de délivrabilité ;
	// ReminderRules les règles de relance. Aucune page encore.
	Suppress      *mailsuppress.Store
	MailDiag      *maildiag.Diagnostic
	ReminderRules *reminderrules.Service
	// GroupPages et AccessPages portent les pages publiques des groupes et de
	// connexion.
	GroupPages  *grouppages.Pages
	AccessPages *accesspages.Pages
	// Bus est le bus d'événements (en mémoire, synchrone).
	Bus ports.EventBus
	// SSRF est la garde de récupération externe (aucune route de recherche encore).
	SSRF *ssrf.Guard
	// Images sert les copies locales d'images sous /images/.
	Images *ssrf.Store
	// Letter est le service des lettres (aucune route encore).
	Letter *letter.Service
	// Vague 8. Invite porte l'inscription par lien (activation, renouvellement,
	// plafonds, formulaire) ; RegCodes les codes de vérification ; Verify la
	// vérification réussie, qui crée le participant et tient le port
	// RegistrationState du tirage (compteur réel des inscriptions en attente).
	// Les pages du formulaire sont minimales, F11c les remplacera.
	Invite   *invite.Service
	RegCodes *code.Service
	Verify   *verify.Service
	// Adjust ajuste le tirage (retrait, ajout) et met la révélation à jour ;
	// aucune page encore (F10c). LetterRead sert la lecture de la lettre par
	// le tireur ; TargetPage les pages du participant.
	Adjust     *Adjusting
	LetterRead *letterread.Service
	TargetPage *targetpage.Pages
	// AdminNav est le registre de navigation d'administration (Tableau,
	// Participants, Tirage, Plus) : F11c y déclarera « Inscriptions ». Audit
	// tient le journal des actions sensibles, Dashboard le tableau de bord.
	AdminNav  *adminnav.Registry
	Audit     *audit.Service
	Dashboard *dashboard.Service
	// Mail est nil tant qu'aucune adresse d'expédition n'est configurée.
	Mail *mailmsg.Builder
	// DKIM est toujours non nil ; Enabled() dit s'il signe.
	DKIM *maildkim.Signer
	// Mux reçoit les routes à chemin fixe (/health, /static/, /images/, /sw.js,
	// /langue, /, ...), derrière la chaîne de middlewares. Il est consulté en
	// premier : aucune route par groupe ne peut masquer l'une d'elles.
	Mux *http.ServeMux
	// Slugs reçoit les routes dont le premier segment est l'adresse d'un
	// groupe (/creer, /{slug}, /{slug}/connexion, ...). Séparé de Mux parce que
	// le multiplexeur refuse qu'un joker de premier segment cohabite avec
	// /static/ ou /images/ ; il n'est consulté que si Mux n'a aucune route.
	Slugs *http.ServeMux

	// regPending et regState sont les magasins SQL de l'inscription (purge).
	regPending *sqlstore.InviteStore
	regState   *sqlstore.VerifyStore

	// mail est l'expéditeur de courriels des fonctionnalités (la file si elle
	// tourne, sinon un refus net : voir mailOutbox).
	mail mailOutbox

	handler  http.Handler
	sched    *scheduler.Scheduler
	mu       sync.Mutex
	closers  []func()
	starters []func(context.Context)
	runWG    sync.WaitGroup
	stop     context.CancelFunc
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
	ipOf, err := newClientIP(cfg.ClientIP)
	if err != nil {
		return nil, fmt.Errorf("adresse du client : %w", err)
	}
	a := &App{
		clientIP: ipOf,
		Cfg:      cfg, DB: database, Clock: clk, Cat: cat, Version: version,
		Policy: policy, Mux: http.NewServeMux(), Slugs: http.NewServeMux(), sched: scheduler.New(clk),
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
		// Seule dispense : le POST « un clic » de désinscription, protégé par
		// la signature de son jeton.
		Exempt: mailunsub.IsOneClickPath,
		OnReject: func(w http.ResponseWriter, r *http.Request, _ error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(cat.T(negotiate.Lang(r.Context(), cfg.DefaultLang), "core.csrf.reject", nil) + "\n"))
		},
	})
	// Ordre : en-têtes, CSRF, sessions, négociation de langue, routes. La
	// langue par défaut du groupe (groupsettings) sert en dernier recours.
	lang := negotiate.Middleware(cat, cfg.DefaultLang, a.GroupSettings)
	a.handler = websec.SecurityHeaders(policy)(csrf.Middleware(a.Sessions.Middleware(lang(a.routes()))))
	return a, nil
}

// routes aiguille une requête : les routes à chemin fixe (Mux) d'abord, les
// routes par groupe (Slugs) ensuite. Une requête qu'aucune route fixe ne
// retient va aux routes par groupe, qui répondent 404 pour tout le reste.
func (a *App) routes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := a.Mux.Handler(r); pattern != "" {
			a.Mux.ServeHTTP(w, r)
			return
		}
		a.Slugs.ServeHTTP(w, r)
	})
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

// OnStart enregistre une tâche de fond lancée par Start ; elle doit rendre la
// main quand son contexte est annulé (Close l'annule puis l'attend).
func (a *App) OnStart(f func(ctx context.Context)) {
	a.mu.Lock()
	a.starters = append(a.starters, f)
	a.mu.Unlock()
}

// Start lance l'ordonnanceur et les tâches de fond ; ils s'arrêtent avec ctx.
func (a *App) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.stop = cancel
	fs := make([]func(context.Context), len(a.starters))
	copy(fs, a.starters)
	a.mu.Unlock()
	go a.sched.Run(ctx, time.Minute)
	for _, f := range fs {
		a.runWG.Add(1)
		go func() {
			defer a.runWG.Done()
			f(ctx)
		}()
	}
}

// Close annule les tâches de fond, les attend, puis exécute les arrêts
// enregistrés (attente des envois en cours, etc.).
func (a *App) Close() {
	a.mu.Lock()
	cancel := a.stop
	cs := a.closers
	a.closers = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.runWG.Wait()
	for i := len(cs) - 1; i >= 0; i-- {
		cs[i]()
	}
}
