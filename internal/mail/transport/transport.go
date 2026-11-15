// Package transport envoie les messages fabriqués par mailmsg sur un serveur
// SMTP générique (net/smtp), selon la configuration de F1.
package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/shared/clock"
)

// ErrNotConfigured est renvoyée quand aucun serveur SMTP n'est configuré.
var ErrNotConfigured = errors.New("courrier non configuré")

// ErrInsecureRefused est renvoyée pour le mode sans TLS sans MAIL_ALLOW_INSECURE.
var ErrInsecureRefused = errors.New("le mode sans TLS exige MAIL_ALLOW_INSECURE=true")

// ErrInvalidMessage signale un message mal formé (injection d'en-têtes, adresse invalide).
var ErrInvalidMessage = errors.New("message invalide")

// Signer signe le message fabriqué (maildkim.Signer).
type Signer interface {
	Sign(msg []byte) ([]byte, error)
}

// Options configure le transport.
type Options struct {
	SMTP config.SMTP
	Mail config.Mail
	// Signer est facultatif ; un message déjà signé n'est pas signé deux fois.
	Signer Signer
	// Clock sert au débit maximal ; Sleep attend (par défaut un minuteur réel).
	Clock clock.Clock
	Sleep func(ctx context.Context, d time.Duration) error
	// TLSConfig remplace la configuration TLS (tests : autorité de confiance).
	TLSConfig *tls.Config
	// Timeout borne la connexion et chaque échange (30 s par défaut).
	Timeout time.Duration
}

// Transport implémente queue.Transport : une seule connexion à la fois.
type Transport struct {
	o    Options
	mode string

	mu   sync.Mutex
	last time.Time
}

var _ queue.Transport = (*Transport)(nil)

// FromConfig renvoie nil (interface nulle) quand le courrier n'est pas configuré,
// afin que la file passe les messages à « courrier non configuré ».
func FromConfig(cfg *config.Config, signer Signer, clk clock.Clock) (queue.Transport, error) {
	if !cfg.SMTP.Enabled() {
		return nil, nil
	}
	t, err := New(Options{SMTP: cfg.SMTP, Mail: cfg.Mail, Signer: signer, Clock: clk})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// New valide la configuration et construit le transport.
func New(o Options) (*Transport, error) {
	mode := strings.ToLower(strings.TrimSpace(o.Mail.TLSMode))
	if mode == "" {
		mode = config.TLSStartTLS
	}
	switch mode {
	case "starttls", "tls":
	case "none":
		if !o.Mail.AllowInsecure {
			return nil, ErrInsecureRefused
		}
	default:
		return nil, fmt.Errorf("mode TLS inconnu %q", mode)
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.SMTP.Port == 0 {
		o.SMTP.Port = 587
	}
	return &Transport{o: o, mode: mode}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// String ne révèle jamais le mot de passe.
func (t *Transport) String() string {
	return fmt.Sprintf("transport SMTP %s:%d (%s)", t.o.SMTP.Host, t.o.SMTP.Port, t.mode)
}

// Format garantit qu'aucun verbe (%v, %+v, %#v) n'expose la configuration.
func (t *Transport) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(t.String())) }

// Send envoie l'enveloppe sur une connexion dédiée, en respectant le débit maximal.
func (t *Transport) Send(ctx context.Context, env queue.Envelope) error {
	if t == nil || !t.o.SMTP.Enabled() {
		return ErrNotConfigured
	}
	from := env.From
	if t.o.Mail.EnvelopeFrom != "" {
		from = t.o.Mail.EnvelopeFrom
	}
	if from == "" {
		from = t.o.SMTP.From
	}
	if err := checkAddr(from); err != nil {
		return err
	}
	if err := checkAddr(env.To); err != nil {
		return err
	}
	raw := env.Raw
	if err := checkHeaders(raw); err != nil {
		return err
	}
	if t.o.Signer != nil && !hasHeader(raw, "DKIM-Signature") {
		signed, err := t.o.Signer.Sign(raw)
		if err != nil {
			return fmt.Errorf("signature DKIM : %w", err)
		}
		raw = signed
	}

	t.mu.Lock() // une seule connexion SMTP à la fois
	defer t.mu.Unlock()
	if err := t.throttle(ctx); err != nil {
		return err
	}
	err := t.deliver(ctx, from, env.To, raw)
	t.last = t.o.Clock.Now()
	return err
}

func (t *Transport) throttle(ctx context.Context) error {
	n := t.o.Mail.MaxPerMinute
	if n <= 0 || t.last.IsZero() {
		return nil
	}
	gap := time.Minute / time.Duration(n)
	if wait := gap - t.o.Clock.Now().Sub(t.last); wait > 0 {
		return t.o.Sleep(ctx, wait)
	}
	return nil
}

func (t *Transport) deliver(ctx context.Context, from, to string, raw []byte) error {
	host := t.o.SMTP.Host
	addr := net.JoinHostPort(host, strconv.Itoa(t.o.SMTP.Port))
	d := net.Dialer{Timeout: t.o.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connexion SMTP impossible : %w", err)
	}
	cfg := t.tlsConfig()
	if t.mode == "tls" {
		conn = tls.Client(conn, cfg)
	}
	stop := afterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(t.o.Timeout))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("accueil SMTP : %w", err)
	}
	defer func() { _ = c.Close() }()
	if h := t.o.Mail.EHLOHostname; h != "" {
		if err := c.Hello(h); err != nil {
			return err
		}
	}
	if t.mode == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("le serveur n'offre pas STARTTLS : envoi refusé")
		}
		if err := c.StartTLS(cfg); err != nil {
			return err
		}
	}
	if t.o.SMTP.User != "" {
		if err := c.Auth(plainAuth{user: t.o.SMTP.User, pass: t.o.SMTP.Password}); err != nil {
			return redactErr(err, t.o.SMTP.Password.Reveal())
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (t *Transport) tlsConfig() *tls.Config {
	var cfg *tls.Config
	if t.o.TLSConfig != nil {
		cfg = t.o.TLSConfig.Clone()
	} else {
		cfg = &tls.Config{}
	}
	cfg.ServerName = t.o.SMTP.Host
	if cfg.MinVersion == 0 {
		cfg.MinVersion = tls.VersionTLS12
	}
	return cfg
}

// afterFunc ferme la connexion si le contexte est annulé.
func afterFunc(ctx context.Context, f func()) func() {
	stop := context.AfterFunc(ctx, f)
	return func() { stop() }
}

// plainAuth est AUTH PLAIN ; le chiffrement est déjà garanti (ou explicitement
// désactivé) par le mode TLS, donc aucune garde supplémentaire ici.
type plainAuth struct {
	user string
	pass config.Secret
}

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass.Reveal()), nil
}

func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("réponse AUTH inattendue")
	}
	return nil, nil
}

// redactErr retire le mot de passe d'un message d'erreur en conservant le code SMTP.
func redactErr(err error, secret string) error {
	if secret == "" || err == nil || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "***"))
}

// checkAddr refuse tout ce qui ressemble à une injection dans une adresse d'enveloppe.
func checkAddr(a string) error {
	if a == "" || strings.ContainsAny(a, "\r\n\x00 \t<>,;\"") || !strings.Contains(a, "@") {
		return fmt.Errorf("%w : adresse d'enveloppe", ErrInvalidMessage)
	}
	return nil
}

// checkHeaders vérifie l'en-tête du message : lignes CRLF seulement, chaque ligne
// est un en-tête « Nom: valeur » ou une continuation repliée ; aucun CR ou LF nu.
func checkHeaders(raw []byte) error {
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return fmt.Errorf("%w : en-tête non terminé", ErrInvalidMessage)
	}
	head := raw[:end]
	if bytes.ContainsRune(head, 0) {
		return fmt.Errorf("%w : octet nul", ErrInvalidMessage)
	}
	for _, line := range bytes.Split(head, []byte("\r\n")) {
		if bytes.ContainsAny(line, "\r\n") {
			return fmt.Errorf("%w : retour à la ligne nu dans un en-tête", ErrInvalidMessage)
		}
		if len(line) == 0 {
			return fmt.Errorf("%w : ligne d'en-tête vide", ErrInvalidMessage)
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		i := bytes.IndexByte(line, ':')
		if i <= 0 || bytes.ContainsAny(line[:i], " \t") {
			return fmt.Errorf("%w : ligne d'en-tête illisible", ErrInvalidMessage)
		}
	}
	return nil
}

func hasHeader(raw []byte, name string) bool {
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		end = len(raw)
	}
	prefix := strings.ToLower(name) + ":"
	for _, line := range bytes.Split(raw[:end], []byte("\r\n")) {
		if strings.HasPrefix(strings.ToLower(string(line)), prefix) {
			return true
		}
	}
	return false
}
