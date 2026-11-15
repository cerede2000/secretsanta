package maildiag

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
)

// fakeResolver est la doublure du résolveur DNS ; elle journalise les noms.
type fakeResolver struct {
	mu   sync.Mutex
	txt  map[string][]string
	mx   map[string][]*net.MX
	err  error
	seen []string
}

func newResolver() *fakeResolver {
	return &fakeResolver{txt: map[string][]string{}, mx: map[string][]*net.MX{}}
}

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, name)
	if f.err != nil {
		return nil, f.err
	}
	r, ok := f.txt[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	return r, nil
}

func (f *fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, name)
	if f.err != nil {
		return nil, f.err
	}
	r, ok := f.mx[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	return r, nil
}

func baseConfig() *config.Config {
	return &config.Config{
		PublicURL: "https://noel.example.org",
		SMTP:      config.SMTP{Host: "smtp.example.org", Port: 587, From: "noel@example.org"},
		Mail:      config.Mail{TLSMode: config.TLSStartTLS},
	}
}

func newDiag(cfg *config.Config, r Resolver) *Diagnostic {
	return New(Options{Config: cfg, Resolver: r})
}

func mustCheck(t *testing.T, rep Report, id string) Check {
	t.Helper()
	c, ok := rep.Check(id)
	if !ok {
		t.Fatalf("point %q absent du rapport", id)
	}
	return c
}

func writeRSAKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	path := filepath.Join(t.TempDir(), "dkim.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, k
}

// pki : autorité de test et certificats.
type pki struct {
	ca     *x509.Certificate
	caKey  *ecdsa.PrivateKey
	pool   *x509.CertPool
	serial int64
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(48 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	ca, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &pki{ca: ca, caKey: key, pool: pool, serial: 1}
}

func (p *pki) leaf(t *testing.T, ip string, notAfter time.Time) tls.Certificate {
	t.Helper()
	p.serial++
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(p.serial), Subject: pkix.Name{CommonName: "leaf"},
		NotBefore: time.Now().Add(-47 * time.Hour), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP(ip)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, p.ca, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// smtpServer est un serveur SMTP minimal : implicit (TLS d'emblée), starttls
// ou plain (sans annoncer STARTTLS).
type smtpServer struct {
	ln net.Listener
}

func startSMTP(t *testing.T, mode string, cert tls.Certificate) *smtpServer {
	t.Helper()
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "implicit" {
		ln = tls.NewListener(ln, cfg)
	}
	s := &smtpServer{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSMTP(c, mode, cfg)
		}
	}()
	return s
}

func (s *smtpServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func serveSMTP(c net.Conn, mode string, cfg *tls.Config) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte("220 test ESMTP\r\n"))
	buf := make([]byte, 512)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		line := strings.ToUpper(strings.TrimSpace(string(buf[:n])))
		switch {
		case strings.HasPrefix(line, "EHLO"):
			if mode == "starttls" {
				_, _ = c.Write([]byte("250-test\r\n250 STARTTLS\r\n"))
			} else {
				_, _ = c.Write([]byte("250 test\r\n"))
			}
		case line == "STARTTLS":
			_, _ = c.Write([]byte("220 go ahead\r\n"))
			tc := tls.Server(c, cfg)
			if tc.Handshake() != nil {
				return
			}
			c = tc
		case line == "QUIT":
			_, _ = c.Write([]byte("221 bye\r\n"))
			return
		default:
			_, _ = c.Write([]byte("250 ok\r\n"))
		}
	}
}
