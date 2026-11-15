package mailmsg

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/clock"
)

// DefaultFromName est le nom d'affichage utilisé quand aucun n'est configuré
// (MAIL_FROM_NAME vide).
const DefaultFromName = "Père Noël secret"

// AutoResponseSuppress est la valeur de X-Auto-Response-Suppress.
const AutoResponseSuppress = "All"

// Options configure le constructeur.
type Options struct {
	// PublicURL est l'adresse publique de l'application : son hôte est le
	// seul autorisé dans les liens.
	PublicURL string
	// From est l'adresse d'expédition (en-tête From).
	From string
	// FromName est le nom d'affichage ; DefaultFromName s'il est vide.
	FromName string
	// EnvelopeFrom est l'expéditeur d'enveloppe (MAIL FROM) ; From s'il est
	// vide. Son domaine doit être aligné sur celui de From.
	EnvelopeFrom string
	// ReplyTo est l'adresse de réponse facultative.
	ReplyTo string
	// Clock fournit la date ; horloge réelle si nil.
	Clock clock.Clock
	// Random fournit l'aléa du Message-ID et des frontières ; crypto/rand si
	// nil.
	Random io.Reader
}

// OptionsFromConfig dérive les options de la configuration validée.
func OptionsFromConfig(c *config.Config, clk clock.Clock, rnd io.Reader) Options {
	return Options{
		PublicURL:    c.PublicURL,
		From:         c.SMTP.From,
		FromName:     c.SMTP.FromName,
		EnvelopeFrom: c.Mail.EnvelopeFrom,
		ReplyTo:      c.Mail.ReplyTo,
		Clock:        clk,
		Random:       rnd,
	}
}

// Builder fabrique des messages conformes.
type Builder struct {
	publicHost   string
	allowHTTP    bool
	from         string
	fromName     string
	envelopeFrom string
	replyTo      string
	clock        clock.Clock
	random       io.Reader
}

// New valide les options et renvoie un constructeur.
func New(o Options) (*Builder, error) {
	u, err := url.Parse(o.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fail(ErrConfig, "L'adresse publique de l'application est invalide.")
	}
	from, err := checkAddress(o.From, "L'adresse d'expédition")
	if err != nil {
		return nil, err
	}
	name := o.FromName
	if strings.TrimSpace(name) == "" {
		name = DefaultFromName
	}
	if name, err = checkName(name, "Le nom d'expéditeur"); err != nil {
		return nil, err
	}
	env := o.EnvelopeFrom
	if env == "" {
		env = from
	}
	if env, err = checkAddress(env, "L'expéditeur d'enveloppe"); err != nil {
		return nil, err
	}
	if !aligned(domainOf(from), domainOf(env)) {
		return nil, fail(ErrAddress, fmt.Sprintf("Le domaine de l'adresse d'expédition (%s) doit être aligné sur celui de l'expéditeur d'enveloppe (%s).", domainOf(from), domainOf(env)))
	}
	replyTo := ""
	if o.ReplyTo != "" {
		if replyTo, err = checkAddress(o.ReplyTo, "L'adresse de réponse"); err != nil {
			return nil, err
		}
	}
	clk := o.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	rnd := o.Random
	if rnd == nil {
		rnd = rand.Reader
	}
	return &Builder{
		publicHost: u.Host, allowHTTP: u.Scheme == "http" && isLoopbackHost(u.Hostname()), from: from, fromName: name, envelopeFrom: env,
		replyTo: replyTo, clock: clk, random: rnd,
	}, nil
}

// Message est le contenu d'un courriel à fabriquer.
type Message struct {
	// To est l'adresse du destinataire ; ToName son nom facultatif.
	To     string
	ToName string
	// Subject est l'objet, contrôlé par LintSubject.
	Subject string
	// Text est la version texte ; HTML la version HTML (fragment de corps ou
	// document complet en lang="fr"). Les deux doivent être équivalentes.
	Text string
	HTML string
	// Essential marque un courriel qu'aucune préférence ne coupe : il ne
	// porte jamais d'en-tête List-Unsubscribe.
	Essential bool
	// UnsubscribeURL (https) et UnsubscribeMailto (mailto:) sont les
	// adresses de désinscription d'un courriel non essentiel.
	UnsubscribeURL    string
	UnsubscribeMailto string
}

// Built est un message prêt à signer et à envoyer.
type Built struct {
	// EnvelopeFrom est l'expéditeur d'enveloppe (MAIL FROM).
	EnvelopeFrom string
	// To est le destinataire d'enveloppe (RCPT TO).
	To string
	// MessageID est la valeur de l'en-tête Message-ID, chevrons compris.
	MessageID string
	// Raw est le message complet, lignes terminées par CRLF.
	Raw []byte
}

// Policy renvoie la politique de liens appliquée à m.
func (b *Builder) Policy(m Message) Policy {
	return Policy{PublicHost: b.publicHost, AllowHTTP: b.allowHTTP, UnsubscribeURL: m.UnsubscribeURL, UnsubscribeMailto: m.UnsubscribeMailto}
}

// Build valide et fabrique le message.
func (b *Builder) Build(m Message) (*Built, error) {
	to, err := checkAddress(m.To, "L'adresse du destinataire")
	if err != nil {
		return nil, err
	}
	toName, err := checkName(m.ToName, "Le nom du destinataire")
	if err != nil {
		return nil, err
	}
	subject, err := normalizeSubject(m.Subject)
	if err != nil {
		return nil, err
	}
	if err := LintSubject(subject); err != nil {
		return nil, err
	}
	if err := checkUnsubscribe(m); err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.Text) == "" || strings.TrimSpace(m.HTML) == "" {
		return nil, fail(ErrBody, "Le courriel doit avoir une version texte et une version HTML.")
	}
	if !utf8.ValidString(m.Text) || !utf8.ValidString(m.HTML) {
		return nil, fail(ErrBody, "Le corps du courriel n'est pas en UTF-8 valide.")
	}
	p := b.Policy(m)
	if err := LintText(m.Text, p); err != nil {
		return nil, err
	}
	if err := LintHTML(m.HTML, p); err != nil {
		return nil, err
	}
	if err := ParityCheck(m.Text, m.HTML); err != nil {
		return nil, err
	}

	idBytes := make([]byte, 16)
	if _, err := io.ReadFull(b.random, idBytes); err != nil {
		return nil, fmt.Errorf("mailmsg: aléa indisponible : %w", err)
	}
	boundaryBytes := make([]byte, 16)
	if _, err := io.ReadFull(b.random, boundaryBytes); err != nil {
		return nil, fmt.Errorf("mailmsg: aléa indisponible : %w", err)
	}
	messageID := "<" + hex.EncodeToString(idBytes) + "@" + domainOf(b.from) + ">"
	boundary := "=_" + hex.EncodeToString(boundaryBytes)

	headers := []header{{"From", formatAddress("From", b.fromName, b.from)}}
	if b.replyTo != "" {
		headers = append(headers, header{"Reply-To", b.replyTo})
	}
	headers = append(headers,
		header{"To", formatAddress("To", toName, to)},
		header{"Subject", encodeWords(subject, len("Subject: "))},
		header{"Date", b.clock.Now().Format(time.RFC1123Z)},
		header{"Message-ID", messageID},
		header{"MIME-Version", "1.0"},
		header{"Content-Language", "fr"},
		header{"Auto-Submitted", "auto-generated"},
		header{"X-Auto-Response-Suppress", AutoResponseSuppress},
	)
	if !m.Essential && m.UnsubscribeURL != "" {
		v := "<" + m.UnsubscribeURL + ">"
		if m.UnsubscribeMailto != "" {
			v += ", <" + m.UnsubscribeMailto + ">"
		}
		headers = append(headers,
			header{"List-Unsubscribe", v},
			header{"List-Unsubscribe-Post", "List-Unsubscribe=One-Click"},
		)
	}
	headers = append(headers, header{"Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": boundary})})

	var out bytes.Buffer
	for _, h := range headers {
		out.WriteString(fold(h.name, h.value))
	}
	out.WriteString("\r\n")

	mw := multipart.NewWriter(&out)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, fmt.Errorf("mailmsg: frontière invalide : %w", err)
	}
	if err := writePart(mw, "text/plain", m.Text); err != nil {
		return nil, err
	}
	if err := writePart(mw, "text/html", wrapHTML(m.HTML, subject)); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	out.WriteString("\r\n")
	return &Built{EnvelopeFrom: b.envelopeFrom, To: to, MessageID: messageID, Raw: out.Bytes()}, nil
}

// checkUnsubscribe valide les adresses de désinscription déclarées.
func checkUnsubscribe(m Message) error {
	if m.UnsubscribeURL != "" {
		if err := checkField(m.UnsubscribeURL, "Le lien de désinscription"); err != nil {
			return err
		}
		u, err := url.Parse(m.UnsubscribeURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(m.UnsubscribeURL, " <>,\"") {
			return fail(ErrLink, "Le lien de désinscription doit être une adresse https complète.")
		}
		if isShortener(u.Hostname()) {
			return fail(ErrLink, "Le lien de désinscription ne doit pas passer par un raccourcisseur d'adresse.")
		}
	}
	if m.UnsubscribeMailto != "" {
		if err := checkField(m.UnsubscribeMailto, "Le mailto de désinscription"); err != nil {
			return err
		}
		if m.UnsubscribeURL == "" {
			return fail(ErrLink, "Le mailto de désinscription accompagne obligatoirement un lien https de désinscription.")
		}
		u, err := url.Parse(m.UnsubscribeMailto)
		if err != nil || u.Scheme != "mailto" || u.Opaque == "" || strings.ContainsAny(m.UnsubscribeMailto, " <>,\"") {
			return fail(ErrLink, "Le mailto de désinscription doit être de la forme mailto:adresse.")
		}
		if _, err := checkAddress(u.Opaque, "Le mailto de désinscription"); err != nil {
			return err
		}
	}
	return nil
}

// writePart écrit une partie UTF-8 en quoted-printable (lignes d'au plus 76
// caractères, fins de ligne CRLF).
func writePart(mw *multipart.Writer, mediaType, body string) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", mediaType+"; charset=UTF-8")
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	w, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	qp := quotedprintable.NewWriter(w)
	if _, err := io.WriteString(qp, body); err != nil {
		return err
	}
	return qp.Close()
}

// wrapHTML enveloppe un fragment dans un document complet en lang="fr" ;
// un document complet est laissé tel quel.
func wrapHTML(src, subject string) string {
	if hasHTMLElement(src) {
		return src
	}
	return "<!DOCTYPE html>\n<html lang=\"fr\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + html.EscapeString(subject) + "</title>\n</head>\n<body>\n" +
		src + "\n</body>\n</html>\n"
}

// hasHTMLElement rapporte si src contient une balise <html>.
func hasHTMLElement(src string) bool {
	z := xhtml.NewTokenizer(strings.NewReader(src))
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return false
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			if name, _ := z.TagName(); string(name) == "html" {
				return true
			}
		}
	}
}

// isLoopbackHost indique si l'hôte (sans port) est localhost, 127.0.0.1 ou ::1.
func isLoopbackHost(h string) bool {
	switch strings.ToLower(h) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
