package maildiag

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/smtp"
	"strconv"

	"secretsanta/internal/config"
)

func (d *Diagnostic) tlsConfig(host string) *tls.Config {
	var c *tls.Config
	if d.tlsCfg != nil {
		c = d.tlsCfg.Clone()
	} else {
		c = &tls.Config{}
	}
	c.ServerName = host
	c.Time = d.clk.Now
	return c
}

// classifyTLS traduit une erreur de poignée de main en variante de code.
func classifyTLS(err error) string {
	var inv x509.CertificateInvalidError
	var host x509.HostnameError
	var unk x509.UnknownAuthorityError
	switch {
	case errors.As(err, &inv) && inv.Reason == x509.Expired:
		return "cert_expired"
	case errors.As(err, &host):
		return "hostname"
	case errors.As(err, &unk):
		return "untrusted"
	}
	// Selon la plate-forme, le vérificateur système renvoie une autre erreur.
	var cv *tls.CertificateVerificationError
	if errors.As(err, &cv) {
		return "untrusted"
	}
	return "handshake_failed"
}

func (d *Diagnostic) checkSMTPTLS(ctx context.Context) Check {
	host := d.cfg.SMTP.Host
	port := d.cfg.SMTP.Port
	if port == 0 {
		port = 587
	}
	mode := d.cfg.Mail.TLSMode
	if mode == "" {
		mode = config.TLSStartTLS
	}
	p := map[string]string{"host": host, "port": strconv.Itoa(port), "mode": mode}

	cctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(cctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return mk(CheckSMTPTLS, StatusError, "unreachable", p, nil)
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := cctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cfg := d.tlsConfig(host)

	if mode == config.TLSImplicit {
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(cctx); err != nil {
			return mk(CheckSMTPTLS, StatusError, classifyTLS(err), p, nil)
		}
		return mk(CheckSMTPTLS, StatusOK, "ok", p, nil)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return mk(CheckSMTPTLS, StatusError, "unreachable", p, nil)
	}
	defer func() { _ = c.Close() }()
	name := d.cfg.Mail.EHLOHostname
	if name == "" {
		name = "localhost"
	}
	if err := c.Hello(name); err != nil {
		return mk(CheckSMTPTLS, StatusError, "unreachable", p, nil)
	}
	offered, _ := c.Extension("STARTTLS")
	if mode == config.TLSNone {
		if offered {
			p["offered"] = "true"
		} else {
			p["offered"] = "false"
		}
		return mk(CheckSMTPTLS, StatusWarn, "none_configured", p, nil)
	}
	if !offered {
		return mk(CheckSMTPTLS, StatusError, "no_encryption", p, nil)
	}
	if err := c.StartTLS(cfg); err != nil {
		return mk(CheckSMTPTLS, StatusError, classifyTLS(err), p, nil)
	}
	return mk(CheckSMTPTLS, StatusOK, "ok", p, nil)
}
