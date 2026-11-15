// Package maildkim signe les messages sortants avec DKIM (RFC 6376) quand la
// configuration le demande : rsa-sha256 ou ed25519-sha256 (RFC 8463), en
// canonicalisation relaxed/relaxed, sur la liste d'en-têtes configurée.
//
// Sans configuration DKIM, le signataire ne fait rien : Sign rend le message
// tel quel. La clé privée n'est lue que depuis un fichier ; elle n'apparaît
// jamais dans une erreur, un journal ou une représentation texte du Signer.
package maildkim

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/emersion/go-msgauth/dkim"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/errs"
)

// Causes stables des refus de signature.
var (
	ErrNotAligned = errors.New("maildkim: domaine de signature non aligné sur l'expéditeur")
	ErrBadMessage = errors.New("maildkim: message impossible à signer")
)

// Signer signe les messages. La valeur zéro et le pointeur nil sont des
// signataires désactivés qui laissent les messages intacts.
type Signer struct {
	domain    string
	selector  string
	algorithm string
	headers   []string
	key       crypto.Signer
}

// New construit le signataire à partir de la configuration. Sans
// configuration DKIM (cfg.Enabled() faux), il renvoie un signataire désactivé
// sans lire aucun fichier.
func New(cfg config.DKIM) (*Signer, error) {
	if !cfg.Enabled() {
		return &Signer{}, nil
	}
	if cfg.Selector == "" {
		return nil, errs.New(errs.KindInternal, "Configuration DKIM incomplète : le sélecteur est obligatoire.")
	}
	if len(cfg.Headers) == 0 {
		return nil, errs.New(errs.KindInternal, "Configuration DKIM incomplète : la liste des en-têtes signés est vide.")
	}
	hasFrom := false
	for _, h := range cfg.Headers {
		hasFrom = hasFrom || strings.EqualFold(h, "From")
	}
	if !hasFrom {
		return nil, errs.New(errs.KindInternal, "Configuration DKIM invalide : l'en-tête From doit être signé.")
	}
	key, err := LoadKey(cfg.KeyFile, cfg.Algorithm)
	if err != nil {
		return nil, err
	}
	return &Signer{
		domain:    normDomain(cfg.Domain),
		selector:  cfg.Selector,
		algorithm: cfg.Algorithm,
		headers:   append([]string(nil), cfg.Headers...),
		key:       key,
	}, nil
}

// Enabled indique si le signataire signe réellement.
func (s *Signer) Enabled() bool { return s != nil && s.key != nil }

// Sign renvoie le message précédé de son en-tête DKIM-Signature. Désactivé,
// il renvoie msg sans le modifier.
//
// Les fins de ligne nues (LF) sont converties en CRLF avant la signature :
// le message renvoyé est celui qui a été signé et doit être envoyé tel quel.
// La signature est refusée (ErrNotAligned) si le domaine de l'adresse From
// n'est pas le domaine de signature ou l'un de ses sous-domaines.
func (s *Signer) Sign(msg []byte) ([]byte, error) {
	if !s.Enabled() {
		return msg, nil
	}
	msg = toCRLF(msg)
	fromDomain, err := fromDomain(msg)
	if err != nil {
		return nil, err
	}
	if !aligned(s.domain, fromDomain) {
		return nil, errs.Wrap(errs.KindValidation, fmt.Sprintf(
			"Signature DKIM refusée : le domaine de signature %q ne s'aligne pas sur le domaine de l'adresse d'expédition %q.",
			s.domain, fromDomain), ErrNotAligned)
	}
	var out bytes.Buffer
	err = dkim.Sign(&out, bytes.NewReader(msg), &dkim.SignOptions{
		Domain:                 s.domain,
		Selector:               s.selector,
		Signer:                 s.key,
		Hash:                   crypto.SHA256,
		HeaderCanonicalization: dkim.CanonicalizationRelaxed,
		BodyCanonicalization:   dkim.CanonicalizationRelaxed,
		HeaderKeys:             s.headers,
	})
	if err != nil {
		// La cause de la bibliothèque n'est pas transmise : elle ne contient
		// pas la clé, mais rien ne le garantit pour ses versions futures.
		return nil, errs.Wrap(errs.KindInternal, "Signature DKIM impossible : le message est mal formé.", ErrBadMessage)
	}
	return out.Bytes(), nil
}

// String ne montre que les paramètres publics. Les méthodes de masquage ont
// un récepteur valeur pour couvrir aussi bien *Signer que Signer.
func (s Signer) String() string {
	if s.key == nil {
		return "maildkim.Signer(désactivé)"
	}
	return fmt.Sprintf("maildkim.Signer(d=%s; s=%s; a=%s)", s.domain, s.selector, s.algorithm)
}

// GoString masque la clé pour %#v.
func (s Signer) GoString() string { return s.String() }

// Format masque la clé pour tous les verbes fmt.
func (s Signer) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(s.String())) }

// LogValue masque la clé pour slog.
func (s Signer) LogValue() slog.Value {
	if s.key == nil {
		return slog.GroupValue(slog.Bool("enabled", false))
	}
	return slog.GroupValue(
		slog.Bool("enabled", true),
		slog.String("domain", s.domain),
		slog.String("selector", s.selector),
		slog.String("algorithm", s.algorithm),
	)
}

// fromDomain lit l'unique en-tête From et renvoie le domaine de son adresse.
func fromDomain(msg []byte) (string, error) {
	bad := func(m string) error { return errs.Wrap(errs.KindValidation, m, ErrBadMessage) }
	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		return "", bad("Signature DKIM impossible : les en-têtes du message sont illisibles.")
	}
	froms := m.Header["From"]
	if len(froms) != 1 {
		return "", bad("Signature DKIM impossible : le message doit porter exactement un en-tête From.")
	}
	addrs, err := mail.ParseAddressList(froms[0])
	if err != nil || len(addrs) != 1 {
		return "", bad("Signature DKIM impossible : l'en-tête From doit contenir exactement une adresse valide.")
	}
	at := strings.LastIndexByte(addrs[0].Address, '@')
	if at < 0 || at == len(addrs[0].Address)-1 {
		return "", bad("Signature DKIM impossible : l'adresse d'expédition n'a pas de domaine.")
	}
	return normDomain(addrs[0].Address[at+1:]), nil
}

// aligned applique l'alignement : domaine identique, ou adresse d'expédition
// dans un sous-domaine du domaine de signature.
func aligned(signing, from string) bool {
	if signing == "" || from == "" {
		return false
	}
	return from == signing || strings.HasSuffix(from, "."+signing)
}

func normDomain(d string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
}

// toCRLF convertit chaque LF non précédé de CR en CRLF.
func toCRLF(msg []byte) []byte {
	if !bytes.Contains(msg, []byte("\n")) {
		return msg
	}
	out := make([]byte, 0, len(msg)+bytes.Count(msg, []byte("\n")))
	for i, c := range msg {
		if c == '\n' && (i == 0 || msg[i-1] != '\r') {
			out = append(out, '\r')
		}
		out = append(out, c)
	}
	return out
}
