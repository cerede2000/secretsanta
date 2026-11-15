package config

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

var domainRe = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

func validDomain(d string) bool { return len(d) <= 253 && domainRe.MatchString(d) }

// validEHLO accepte un nom de domaine plein ou une adresse entre crochets.
func validEHLO(h string) bool {
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		inner := h[1 : len(h)-1]
		inner = strings.TrimPrefix(inner, "IPv6:")
		return net.ParseIP(inner) != nil
	}
	return validDomain(h)
}

func (l *loader) loadMail(c *Config) {
	m := &c.Mail
	m.EHLOHostname = l.get(VarMailEHLOHostname)
	if m.EHLOHostname != "" && !validEHLO(m.EHLOHostname) {
		l.fail("%s doit être un nom de domaine plein (mail.example.org) ou une adresse entre crochets ([203.0.113.7]).", VarMailEHLOHostname)
	}

	m.EnvelopeFrom = l.str(VarMailEnvelopeFrom, c.SMTP.From)
	if v := l.get(VarMailEnvelopeFrom); v != "" && !validEmail(v) {
		l.fail("%s n'est pas une adresse courriel valide.", VarMailEnvelopeFrom)
	}
	m.ReplyTo = l.get(VarMailReplyTo)
	if m.ReplyTo != "" && !validEmail(m.ReplyTo) {
		l.fail("%s n'est pas une adresse courriel valide.", VarMailReplyTo)
	}

	m.TLSMode = strings.ToLower(l.str(VarMailTLSMode, TLSStartTLS))
	switch m.TLSMode {
	case TLSStartTLS, TLSImplicit, TLSNone:
	default:
		l.fail("%s doit valoir starttls, tls ou none.", VarMailTLSMode)
	}
	if v := l.get(VarMailAllowInsecure); v != "" {
		b, ok := parseBool(v)
		if !ok {
			l.fail("%s doit valoir true ou false.", VarMailAllowInsecure)
		}
		m.AllowInsecure = b
	}
	if m.TLSMode == TLSNone && !m.AllowInsecure {
		l.fail("%s=none est refusé : le chiffrement est exigé par défaut ; pour l'autoriser explicitement, définir %s=true.", VarMailTLSMode, VarMailAllowInsecure)
	}

	m.MaxPerMinute = DefaultMaxPerMinute
	if v := l.get(VarMailMaxPerMinute); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10000 {
			l.fail("%s doit être un entier entre 1 et 10000.", VarMailMaxPerMinute)
		} else {
			m.MaxPerMinute = n
		}
	}
}
