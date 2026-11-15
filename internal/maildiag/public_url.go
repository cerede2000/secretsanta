package maildiag

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkPublicURL contrôle le HTTPS (certificat) puis /health de PUBLIC_URL.
func (d *Diagnostic) checkPublicURL(ctx context.Context) []Check {
	u, err := url.Parse(d.cfg.PublicURL)
	if err != nil || u.Host == "" {
		return []Check{mk(CheckPublicHTTPS, StatusError, "invalid", nil, nil)}
	}
	p := map[string]string{"host": u.Hostname()}
	var out []Check
	switch {
	case u.Scheme != "https" && isLoopback(u.Hostname()):
		out = append(out, mk(CheckPublicHTTPS, StatusOff, "local", p, nil))
	case u.Scheme != "https":
		out = append(out, mk(CheckPublicHTTPS, StatusWarn, "not_https", p, nil))
	}

	health := *u
	health.Path = "/health"
	health.RawQuery, health.Fragment = "", ""
	req := (&http.Request{Method: http.MethodGet, URL: &health, Header: http.Header{}, Host: health.Host}).WithContext(ctx)
	resp, err := d.client.Do(req)
	if err != nil {
		v := "unreachable"
		var inv x509.CertificateInvalidError
		var host x509.HostnameError
		var unk x509.UnknownAuthorityError
		switch {
		case errors.As(err, &inv) && inv.Reason == x509.Expired:
			v = "cert_expired"
		case errors.As(err, &host):
			v = "hostname"
		case errors.As(err, &unk):
			v = "untrusted"
		default:
			var cv *tls.CertificateVerificationError
			if errors.As(err, &cv) {
				v = "untrusted"
			}
		}
		if v != "unreachable" {
			out = append(out, mk(CheckPublicHTTPS, StatusError, v, p, nil))
		} else if u.Scheme == "https" {
			out = append(out, mk(CheckPublicHTTPS, StatusError, "unreachable", p, nil))
		}
		return append(out, mk(CheckPublicHealth, StatusError, "unreachable", p, nil))
	}
	defer func() { _ = resp.Body.Close() }()
	if u.Scheme == "https" {
		out = append(out, mk(CheckPublicHTTPS, StatusOK, "ok", p, nil))
	}
	if resp.StatusCode == http.StatusOK {
		return append(out, mk(CheckPublicHealth, StatusOK, "ok", p, nil))
	}
	return append(out, mk(CheckPublicHealth, StatusError, "bad_status", map[string]string{"host": u.Hostname(), "status": strconv.Itoa(resp.StatusCode)}, nil))
}
