package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"secretsanta/internal/mail/queue"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"
	"secretsanta/internal/registration/sqlstore"
	"secretsanta/internal/registration/verify"
	regpublic "secretsanta/internal/registrationpages/public"
	"secretsanta/internal/websec"
)

// mailEnqueuer adapte la file d'envoi à code.Enqueuer et verify.Enqueuer : sans
// transport (la file ne tourne pas), il refuse net au lieu de déréférencer une
// file absente.
type mailEnqueuer struct{ q *queue.Service }

// EnqueueMail met le courriel en file, ou refuse si l'envoi est désactivé.
func (m mailEnqueuer) EnqueueMail(ctx context.Context, it queue.Item) (int64, error) {
	if m.q == nil {
		return 0, ErrMailDisabled
	}
	return m.q.EnqueueMail(ctx, it)
}

// registrationThrottle adapte les plafonds de code.Service au port
// invite.Throttle : 20 soumissions et 5 créations par fenêtre de 15 minutes et
// par adresse IP et par groupe.
type registrationThrottle struct{ svc *code.Service }

// AllowSubmission implémente invite.Throttle.
func (t registrationThrottle) AllowSubmission(_ context.Context, groupID int64, ip string) error {
	return t.svc.AllowSubmission(groupID, ip)
}

// AllowRegistration implémente invite.Throttle.
func (t registrationThrottle) AllowRegistration(_ context.Context, groupID int64, ip, _ string) error {
	return t.svc.AllowRegistration(groupID, ip)
}

var _ invite.Throttle = registrationThrottle{}

// registerRegistration construit l'inscription par lien : magasins SQL, codes
// de vérification, vérification (qui tient aussi le port RegistrationState du
// tirage : compteur réel des inscriptions en attente, hors ignorées),
// formulaire. Elle précède le tirage, qui en a besoin, et suit les jetons
// d'accès, l'enrôlement et la file d'envoi. Les routes sont montées plus loin
// (registerRegistrationPages). La purge à 48 heures est planifiée ici.
func registerRegistration(a *App) error {
	key := []byte(a.Cfg.SigningKey.Reveal())
	base := a.publicBase()
	enq := mailEnqueuer{q: a.mail.q}
	pending := sqlstore.NewInviteStore(a.DB)
	state := sqlstore.NewVerifyStore(a.DB)

	codes, err := code.New(code.Options{
		Store:  sqlstore.NewCodeStore(a.DB),
		Clock:  a.Clock,
		Mailer: code.TemplateMailer{Catalog: a.Cat, Queue: enq},
		Link:   func(slug, tok string) string { return base + "/" + slug + "/inscription/confirmer/" + tok },
		Key:    key,
	})
	if err != nil {
		return fmt.Errorf("codes de vérification : %w", err)
	}
	ver, err := verify.New(verify.Config{
		Pending: pending, Store: state, Codes: codes, Enrol: a.Enrolment, Groups: a.Groups,
		Draw: a.DrawState, Clock: a.Clock,
		Welcome:   verify.TemplateMailer{Catalog: a.Cat, Queue: enq},
		Scheduler: a.sched, Lang: a.GroupSettings, Logger: slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("vérification des inscriptions : %w", err)
	}
	inv, err := invite.New(invite.Config{
		Groups: a.Groups, Directory: invite.SQLDirectory{DB: a.DB}, Store: pending, Draw: a.DrawState,
		Clock: a.Clock, Key: key, Codes: ver, Throttle: registrationThrottle{svc: codes}, Logger: slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("inscription par lien : %w", err)
	}
	a.Invite, a.RegCodes, a.Verify = inv, codes, ver
	a.regPending, a.regState = pending, state

	// Purge des inscriptions expirées (48 heures) : au démarrage, à
	// l'expiration de chacune, puis périodiquement en filet.
	a.Every("registration.purge", purgeInterval, func(ctx context.Context) error {
		return a.purgeRegistrations(ctx)
	})
	a.OnStart(func(ctx context.Context) {
		if err := a.restoreRegistrations(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("reprise de la purge des inscriptions en échec", "erreur", err)
		}
	})
	return nil
}

// purgeRegistrations retire les inscriptions expirées de tous les groupes qui
// en portent, avec leur code, leur état « ignorée » et leur trace.
func (a *App) purgeRegistrations(ctx context.Context) error {
	ids, err := a.regState.GroupIDs(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if _, err := a.Verify.PurgeExpired(ctx, id); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// restoreRegistrations reprend la purge après un redémarrage : les tâches
// planifiées en mémoire ont été perdues. Il purge ce qui est déjà expiré, puis
// replanifie, pour chaque inscription encore vivante, une purge à son
// expiration.
func (a *App) restoreRegistrations(ctx context.Context) error {
	if err := a.purgeRegistrations(ctx); err != nil {
		return err
	}
	ids, err := a.regState.GroupIDs(ctx)
	if err != nil {
		return err
	}
	now := a.Clock.Now()
	for _, id := range ids {
		// Instant zéro : toutes les inscriptions du groupe.
		ps, err := a.regPending.ListPending(ctx, id, time.Time{})
		if err != nil {
			return err
		}
		for _, p := range ps {
			if !p.ExpiresAt.After(now) {
				continue
			}
			gid := p.GroupID
			a.sched.At(p.ExpiresAt, func(ctx context.Context) error {
				_, err := a.Verify.PurgeExpired(ctx, gid)
				return err
			})
		}
	}
	return nil
}

// signedInVerifier enveloppe la vérification des inscriptions pour les pages
// publiques : quand une vérification par code ou par lien en un clic réussit
// (preuve de possession de la boîte mail), elle note la réussite dans la
// requête, et openSessionAfterVerify ouvre alors la session de la personne.
type signedInVerifier struct{ inner regpublic.Verifier }

type verifiedKey struct{}

// verifiedNote reçoit la réussite d'une vérification ; il vit dans le contexte
// de la requête.
type verifiedNote struct{ success *verify.Success }

// VerifyCode implémente regpublic.Verifier.
func (v signedInVerifier) VerifyCode(ctx context.Context, groupID int64, ref, input string) (*verify.Success, error) {
	s, err := v.inner.VerifyCode(ctx, groupID, ref, input)
	if err == nil {
		noteVerified(ctx, s)
	}
	return s, err
}

// PreviewLink implémente regpublic.Verifier ; l'aperçu n'a aucun effet.
func (v signedInVerifier) PreviewLink(ctx context.Context, groupID int64, tok string) (code.Preview, error) {
	return v.inner.PreviewLink(ctx, groupID, tok)
}

// ConfirmLink implémente regpublic.Verifier.
func (v signedInVerifier) ConfirmLink(ctx context.Context, groupID int64, tok string) (*verify.Success, error) {
	s, err := v.inner.ConfirmLink(ctx, groupID, tok)
	if err == nil {
		noteVerified(ctx, s)
	}
	return s, err
}

var _ regpublic.Verifier = signedInVerifier{}

func noteVerified(ctx context.Context, s *verify.Success) {
	if n, ok := ctx.Value(verifiedKey{}).(*verifiedNote); ok {
		n.success = s
	}
}

// sessionWriter ouvre la session participant juste avant que l'en-tête de la
// réponse parte : le cookie doit précéder le corps.
type sessionWriter struct {
	http.ResponseWriter
	open func()
	done bool
}

func (w *sessionWriter) first() {
	if !w.done {
		w.done = true
		w.open()
	}
}

// WriteHeader implémente http.ResponseWriter.
func (w *sessionWriter) WriteHeader(code int) {
	w.first()
	w.ResponseWriter.WriteHeader(code)
}

// Write implémente http.ResponseWriter.
func (w *sessionWriter) Write(b []byte) (int, error) {
	w.first()
	return w.ResponseWriter.Write(b)
}

// Unwrap laisse http.ResponseController atteindre l'écrivain d'origine.
func (w *sessionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// openSessionAfterVerify enveloppe un gestionnaire de vérification (saisie du
// code, lien de confirmation en un clic) : si la personne vient d'être créée
// par cette vérification, sa session participant est ouverte dans la même
// réponse, car elle a prouvé qu'elle possède la boîte mail. Aucune session
// pour une inscription déjà vérifiée (code rejoué, validation de
// l'administrateur sans code) : rien n'a prouvé la possession de la boîte. Si
// la session ne peut pas s'ouvrir, la personne reste inscrite et se connecte
// comme d'habitude (lien personnel, lien magique).
func (a *App) openSessionAfterVerify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		note := &verifiedNote{}
		r = r.WithContext(context.WithValue(r.Context(), verifiedKey{}, note))
		sw := &sessionWriter{ResponseWriter: w}
		sw.open = func() {
			s := note.success
			if s == nil || s.AlreadyVerified || s.ParticipantID <= 0 {
				return
			}
			_, err := a.Sessions.Create(r.Context(), sw.ResponseWriter, r, websec.Identity{
				Kind: websec.KindParticipant, GroupID: strconv.FormatInt(s.GroupID, 10),
				Subject: strconv.FormatInt(s.ParticipantID, 10),
			})
			if err != nil {
				slog.Error("inscription : session non ouverte", "groupe", s.GroupID, "erreur", err)
			}
		}
		next.ServeHTTP(sw, r)
		sw.first()
	})
}

// registerRegistrationPages monte les pages publiques de l'inscription
// (registrationpages/public : formulaire sous magic.PatternJoin et
// magic.PatternJoinBare, saisie du code, lien de confirmation en un clic,
// écran de réussite) sur Slugs, derrière la chaîne SecurityHeaders, CSRF,
// Sessions. Le paquet analyse un jeu de gabarits par page. L'écran de réussite
// mène à la lettre (/{slug}/lettre?bienvenue=1, le paramètre est ajouté par le
// paquet) ; les deux gestes qui vérifient l'inscription ouvrent la session
// participant (openSessionAfterVerify).
func registerRegistrationPages(a *App) error {
	p, err := regpublic.New(regpublic.Options{
		Groups: a.Groups, Invites: a.Invite, Verify: signedInVerifier{inner: a.Verify}, Translator: a.Cat,
		Logger: slog.Default(), ClientIP: a.clientIP, Location: a.Cfg.Location,
		LetterPath: func(slug string) string { return "/" + slug + "/lettre" },
		Home:       func(slug string) string { return "/" + slug + "/participant" },
	})
	if err != nil {
		return fmt.Errorf("pages d'inscription : %w", err)
	}
	for _, r := range p.Routes() {
		h := r.Handler
		if r.Modifies {
			// Seules les routes qui vérifient l'inscription (POST du code et
			// de la confirmation) modifient l'état.
			h = a.openSessionAfterVerify(h)
		}
		a.Slugs.Handle(r.Pattern, h)
	}
	return nil
}
