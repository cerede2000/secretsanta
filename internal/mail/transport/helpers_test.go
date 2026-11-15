package transport

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/mail/queue"
)

// fakeServer est un serveur SMTP en processus (aucun réseau externe).
type fakeServer struct {
	t        *testing.T
	ln       net.Listener
	mode     string // none, starttls, tls
	noStart  bool   // n'annonce pas STARTTLS
	rcptCode string // réponse à RCPT (défaut 250 OK)
	tlsConf  *tls.Config
	roots    *x509.CertPool

	mu        sync.Mutex
	ehlo      []string
	mailFrom  []string
	rcptTo    []string
	data      [][]byte
	auth      []string
	tlsUsed   []bool
	conns     int
	maxActive int
	active    int
}

func newFakeServer(t *testing.T, mode string) *fakeServer {
	t.Helper()
	s := &fakeServer{t: t, mode: mode}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	s.roots = x509.NewCertPool()
	s.roots.AddCert(cert)
	s.tlsConf = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *fakeServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *fakeServer) handle(c net.Conn) {
	s.mu.Lock()
	s.conns++
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
		_ = c.Close()
	}()
	secure := false
	if s.mode == "tls" {
		c = tls.Server(c, s.tlsConf)
		secure = true
	}
	r := bufio.NewReader(c)
	say := func(f string, a ...any) { fmt.Fprintf(c, f+"\r\n", a...) }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO "), strings.HasPrefix(up, "HELO "):
			s.mu.Lock()
			s.ehlo = append(s.ehlo, line[5:])
			s.mu.Unlock()
			fmt.Fprintf(c, "250-fake\r\n")
			if s.mode == "starttls" && !secure && !s.noStart {
				fmt.Fprintf(c, "250-STARTTLS\r\n")
			}
			say("250 AUTH PLAIN")
		case up == "STARTTLS":
			say("220 go ahead")
			c = tls.Server(c, s.tlsConf)
			r = bufio.NewReader(c)
			secure = true
		case strings.HasPrefix(up, "AUTH PLAIN "):
			raw, _ := base64.StdEncoding.DecodeString(line[11:])
			s.mu.Lock()
			s.auth = append(s.auth, string(raw))
			s.mu.Unlock()
			say("235 ok")
		case strings.HasPrefix(up, "MAIL FROM:"):
			s.mu.Lock()
			s.mailFrom = append(s.mailFrom, strings.Trim(line[10:], "<> "))
			s.tlsUsed = append(s.tlsUsed, secure)
			s.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			s.mu.Lock()
			s.rcptTo = append(s.rcptTo, strings.Trim(line[8:], "<> "))
			s.mu.Unlock()
			if s.rcptCode != "" {
				say("%s", s.rcptCode)
				continue
			}
			say("250 ok")
		case up == "DATA":
			say("354 go")
			var buf strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				buf.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.data = append(s.data, []byte(buf.String()))
			s.mu.Unlock()
			say("250 queued")
		case up == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func baseOpts(s *fakeServer, mode string) Options {
	return Options{
		SMTP:      config.SMTP{Host: "127.0.0.1", Port: s.port(), From: "noel@example.org"},
		Mail:      config.Mail{TLSMode: mode, EHLOHostname: "mail.example.org", EnvelopeFrom: "bounce@example.org"},
		TLSConfig: &tls.Config{RootCAs: s.roots},
		Timeout:   5 * time.Second,
	}
}

func rawMsg(subject string) []byte {
	return []byte("From: noel@example.org\r\nTo: lea@example.net\r\nSubject: " + subject +
		"\r\nMessage-ID: <1@example.org>\r\n\r\nBonjour Léa\r\n.point seul\r\n")
}

func env() queue.Envelope {
	return queue.Envelope{From: "noel@example.org", To: "lea@example.net", Raw: rawMsg("Test")}
}
