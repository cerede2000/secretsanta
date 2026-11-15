// Package magic porte le lien magique de connexion des participants (F2b) :
// demande à réponse uniforme, jeton de 128 bits stocké haché, valable
// 15 minutes et consommé de façon atomique (usage unique), ainsi que la
// réponse neutre du chemin d'invitation (voir invitation.go).
package magic

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/access/limiter"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/shared/random"
)

// TTL est la durée de validité d'un lien magique.
const TTL = 15 * time.Minute

// EmailType est la nature du courriel de lien magique.
const EmailType ports.EmailType = "lien_magique"

// maxEmailLen borne l'adresse saisie (RFC 5321).
const maxEmailLen = 254

// maxBackground borne le nombre de traitements d'arrière-plan simultanés.
const maxBackground = 8

// Directory retrouve un participant d'un groupe par son adresse de courriel
// (implémenté au-dessus des participants de F3, branché par l'orchestrateur).
// La comparaison des adresses (casse, espaces) est de son ressort.
type Directory interface {
	FindByEmail(ctx context.Context, groupID int64, email string) (participantID int64, found bool, err error)
}

// LinkFunc construit l'URL absolue du lien magique à partir du slug du
// groupe et du jeton en clair.
type LinkFunc func(slug, token string) string

// Composer construit le courriel du lien magique (modèle de F6) pour le
// destinataire to et l'URL link.
type Composer func(to, link string) ports.EmailMessage

// Config rassemble les dépendances du service.
type Config struct {
	DB        *sql.DB
	Clock     clock.Clock
	Directory Directory
	Sender    ports.EmailSender
	Link      LinkFunc
	Compose   Composer
	// Limiter est facultatif : par défaut limiter.MagicLinkRequests
	// (5 demandes par 15 minutes, par adresse IP et par cible).
	Limiter *limiter.Limiter
	// Logger est facultatif : slog.Default() par défaut.
	Logger *slog.Logger
}

// Service gère les liens magiques.
type Service struct {
	db      *sql.DB
	clk     clock.Clock
	dir     Directory
	sender  ports.EmailSender
	link    LinkFunc
	compose Composer
	lim     *limiter.Limiter
	log     *slog.Logger

	wg  sync.WaitGroup
	sem chan struct{}
}

// New crée le service.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Clock == nil || cfg.Directory == nil || cfg.Sender == nil || cfg.Link == nil || cfg.Compose == nil {
		return nil, errors.New("magic: dépendance manquante")
	}
	lim := cfg.Limiter
	if lim == nil {
		lim = limiter.New(cfg.Clock, limiter.MagicLinkRequests)
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		db: cfg.DB, clk: cfg.Clock, dir: cfg.Directory, sender: cfg.Sender,
		link: cfg.Link, compose: cfg.Compose, lim: lim, log: log,
		sem: make(chan struct{}, maxBackground),
	}, nil
}

// Request est une demande de lien magique.
type Request struct {
	GroupID int64
	Slug    string
	Email   string
	IP      string
}

// Response est la réponse d'une demande acceptée. Elle est identique que
// l'adresse appartienne ou non à un participant du groupe.
type Response struct {
	Status  int
	Message string
}

// UniformResponse est l'unique réponse d'une demande acceptée.
var UniformResponse = Response{
	Status:  http.StatusOK,
	Message: "Si cette adresse correspond à un participant du groupe, un lien de connexion vient de lui être envoyé. Il est valable 15 minutes.",
}

// RequestLink traite une demande de lien magique. La réponse ne dépend que de
// la forme de l'adresse et du limiteur, jamais de l'existence du
// participant : la recherche, la création du lien et l'envoi se font en
// arrière-plan, après la réponse, pour que la durée de traitement soit la
// même dans les deux cas. Un groupe inconnu se traite avec GroupID 0 (aucun
// participant n'y est trouvé).
//
// Erreurs : validation (adresse illisible) ou limiter.Error() (enveloppe
// limiter.ErrLimited) quand l'adresse IP ou la cible a dépassé 5 demandes
// dans la fenêtre de 15 minutes.
func (s *Service) RequestLink(ctx context.Context, req Request) (Response, error) {
	email, ok := normaliseEmail(req.Email)
	if !ok {
		return Response{}, errs.Validation("Saisis une adresse de courriel valide.")
	}
	if d := s.lim.Allow(limiter.IPKey(req.GroupID, req.IP), limiter.TargetKey(req.GroupID, email)); !d.Allowed {
		return Response{}, limiter.Error()
	}
	bg := context.WithoutCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		s.deliver(bg, req.GroupID, req.Slug, email)
	}()
	return UniformResponse, nil
}

// Wait attend la fin des traitements d'arrière-plan (tests, arrêt propre).
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) deliver(ctx context.Context, groupID int64, slug, email string) {
	pid, found, err := s.dir.FindByEmail(ctx, groupID, email)
	if err != nil {
		s.log.Error("lien magique : recherche du participant", "groupe", groupID, "erreur", err)
		return
	}
	if !found {
		return
	}
	token, err := s.Issue(ctx, groupID, pid)
	if err != nil {
		s.log.Error("lien magique : création du lien", "groupe", groupID, "erreur", err)
		return
	}
	msg := s.compose(email, s.link(slug, token))
	msg.To = email
	msg.Type = EmailType
	msg.Essential = true
	if err := s.sender.Send(ctx, msg); err != nil {
		s.log.Error("lien magique : envoi du courriel", "groupe", groupID, "erreur", err)
	}
}

func normaliseEmail(raw string) (string, bool) {
	e := strings.TrimSpace(raw)
	if e == "" || len(e) > maxEmailLen {
		return "", false
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || a.Name != "" {
		return "", false
	}
	return e, true
}

// Issue crée un lien magique pour un participant et renvoie le jeton en
// clair, qui n'existe ensuite que dans le lien remis. Les liens précédents
// du participant sont révoqués : seul le dernier lien demandé est valable.
func (s *Service) Issue(ctx context.Context, groupID, participantID int64) (string, error) {
	token, err := random.Token()
	if err != nil {
		return "", err
	}
	now := s.clk.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM magic_links WHERE group_id = ? AND participant_id = ?`, groupID, participantID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO magic_links (token_hash, group_id, participant_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), groupID, participantID, now.UnixNano(), now.Add(TTL).UnixNano()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

// invalidLink est l'erreur unique d'un lien inconnu, expiré, déjà utilisé,
// d'un autre groupe ou mal formé : rien ne dit lequel.
func invalidLink() *errs.Error {
	return errs.New(errs.KindUnauthorized, "Ce lien de connexion n'est plus valable. Demande-en un nouveau.")
}

// Consume utilise le lien magique : il renvoie le participant si le jeton est
// valable dans ce groupe et le marque utilisé dans la même instruction SQL,
// si bien que de deux consommations simultanées une seule réussit. Le lien
// expire TTL après sa création (à l'instant exact, il n'est plus valable).
func (s *Service) Consume(ctx context.Context, groupID int64, token string) (int64, error) {
	if !wellFormed(token) {
		return 0, invalidLink()
	}
	now := s.clk.Now().UnixNano()
	var pid int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE magic_links SET used_at = ?
		 WHERE token_hash = ? AND group_id = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING participant_id`,
		now, hashToken(token), groupID, now).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, invalidLink()
	}
	if err != nil {
		return 0, errs.Internal(err)
	}
	return pid, nil
}

// PurgeExpired supprime les liens expirés ou utilisés ; à planifier par
// l'orchestrateur.
func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM magic_links WHERE expires_at <= ? OR used_at IS NOT NULL`, s.clk.Now().UnixNano())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func wellFormed(token string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(b) == random.TokenBytes
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
