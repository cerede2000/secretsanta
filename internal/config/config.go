// Package config déclare et valide toute la configuration de l'application,
// lue exclusivement depuis des variables d'environnement.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // fuseaux embarqués : fonctionne dans une image minimale
)

// Defaults.
const (
	DefaultTimezone     = "Europe/Paris"
	DefaultLang         = "fr"
	DefaultListenAddr   = ":8080"
	DefaultDataDir      = "/data"
	DefaultPublicURL    = "http://localhost:8080"
	DefaultMaxPerMinute = 30
	DefaultDKIMHeaders  = "From,To,Subject,Date,Message-ID,Reply-To,MIME-Version,Content-Type,List-Unsubscribe,List-Unsubscribe-Post"
)

// Config est la configuration validée.
type Config struct {
	PublicURL    string
	ListenAddr   string
	DataDir      string
	Timezone     string
	Location     *time.Location
	DefaultLang  string // langue par défaut (APP_DEFAULT_LANG)
	I18nDir      string // dossier externe de traductions (I18N_DIR), facultatif
	InstancePass Secret
	SigningKey   Secret

	SMTP SMTP
	Mail Mail
	DKIM DKIM

	RemindersEnabled bool

	Sources Sources
}

// SMTP décrit le serveur de courrier. Absent (Host vide), l'envoi est désactivé.
type SMTP struct {
	Host     string
	Port     int
	User     string
	Password Secret
	From     string
	FromName string
}

// Enabled indique si le courrier est configuré.
func (s SMTP) Enabled() bool { return s.Host != "" }

// Mail regroupe les réglages de délivrabilité.
type Mail struct {
	EHLOHostname  string
	EnvelopeFrom  string
	ReplyTo       string
	TLSMode       string
	AllowInsecure bool
	MaxPerMinute  int
}

// DKIM décrit la signature facultative faite par l'application.
type DKIM struct {
	Domain    string
	Selector  string
	KeyFile   Secret // chemin : traité comme secret, jamais journalisé
	Algorithm string
	Headers   []string
}

// Enabled indique si la signature DKIM est configurée.
func (d DKIM) Enabled() bool { return d.Domain != "" }

// Sources regroupe les sources facultatives de suggestions.
type Sources struct {
	SearxngURL string
	BraveKey   Secret
	SerperKey  Secret
}

// Modes TLS.
const (
	TLSStartTLS = "starttls"
	TLSImplicit = "tls"
	TLSNone     = "none"
)

// Algorithmes DKIM.
const (
	AlgRSA     = "rsa-sha256"
	AlgEd25519 = "ed25519-sha256"
)

// Error regroupe tous les problèmes de configuration, en français.
type Error struct{ Problems []string }

func (e *Error) Error() string {
	return "configuration invalide :\n - " + strings.Join(e.Problems, "\n - ")
}

type loader struct {
	getenv   func(string) string
	declared map[string]bool
	problems []string
}

func (l *loader) get(name string) string {
	if !l.declared[name] {
		panic("variable d'environnement non déclarée : " + name)
	}
	return strings.TrimSpace(l.getenv(name))
}

func (l *loader) fail(format string, a ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, a...))
}

func (l *loader) str(name, def string) string {
	if v := l.get(name); v != "" {
		return v
	}
	return def
}

// LoadFromEnv lit la configuration depuis l'environnement du processus.
func LoadFromEnv() (*Config, error) { return Load(os.Getenv) }

// Load construit et valide la configuration. Les messages d'erreur ne
// contiennent jamais de valeur secrète ni de chemin de clé.
func Load(getenv func(string) string) (*Config, error) {
	l := &loader{getenv: getenv, declared: map[string]bool{}}
	for _, v := range Variables {
		l.declared[v] = true
	}
	c := &Config{}

	c.PublicURL = strings.TrimRight(l.str(VarPublicURL, DefaultPublicURL), "/")
	if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		l.fail("%s doit être une adresse http(s) complète (par exemple https://noel.example.org).", VarPublicURL)
	}
	c.ListenAddr = l.str(VarListenAddr, DefaultListenAddr)
	c.DataDir = l.str(VarDataDir, DefaultDataDir)

	for _, r := range []struct {
		name string
		dst  *Secret
		what string
	}{
		{VarInstancePassword, &c.InstancePass, "le mot de passe d'instance (création de groupe)"},
		{VarSigningSecret, &c.SigningKey, "le secret de signature"},
	} {
		v := l.get(r.name)
		if v == "" {
			l.fail("%s est obligatoire : %s n'a pas de valeur par défaut et l'application refuse de démarrer sans lui.", r.name, r.what)
			continue
		}
		*r.dst = NewSecret(v)
	}

	c.Timezone = l.str(VarTimezone, DefaultTimezone)
	if c.Timezone == "Local" {
		l.fail("%s : « Local » n'est pas accepté, indiquer un fuseau IANA (par exemple %s).", VarTimezone, DefaultTimezone)
	} else if loc, err := time.LoadLocation(c.Timezone); err != nil {
		l.fail("%s : fuseau horaire inconnu, indiquer un nom IANA valide (par exemple %s).", VarTimezone, DefaultTimezone)
	} else {
		c.Location = loc
	}

	l.loadLang(c)
	l.loadSMTP(c)
	l.loadMail(c)
	l.loadDKIM(c)

	c.RemindersEnabled = true
	if v := l.get(VarRemindersEnabled); v != "" {
		b, ok := parseBool(v)
		if !ok {
			l.fail("%s doit valoir true ou false.", VarRemindersEnabled)
		}
		c.RemindersEnabled = b
	}

	c.Sources.SearxngURL = strings.TrimRight(l.get(VarSearxngURL), "/")
	if s := c.Sources.SearxngURL; s != "" {
		if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			l.fail("%s doit être une adresse http(s) complète.", VarSearxngURL)
		}
	}
	c.Sources.BraveKey = NewSecret(l.get(VarBraveAPIKey))
	c.Sources.SerperKey = NewSecret(l.get(VarSerperAPIKey))

	if len(l.problems) > 0 {
		return nil, &Error{Problems: l.problems}
	}
	return c, nil
}

func parseBool(v string) (value, ok bool) {
	switch strings.ToLower(v) {
	case "1", "true", "vrai", "yes", "oui":
		return true, true
	case "0", "false", "faux", "no", "non":
		return false, true
	}
	return false, false
}

func (l *loader) loadSMTP(c *Config) {
	s := &c.SMTP
	s.Host = l.get(VarSMTPHost)
	s.User = l.get(VarSMTPUser)
	s.Password = NewSecret(l.get(VarSMTPPassword))
	s.From = l.get(VarMailFrom)
	s.FromName = l.get(VarMailFromName)
	s.Port = 587
	if v := l.get(VarSMTPPort); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			l.fail("%s doit être un numéro de port entre 1 et 65535.", VarSMTPPort)
		} else {
			s.Port = p
		}
	}
	if s.Host == "" {
		return // courrier désactivé : ce n'est pas une erreur
	}
	if s.From == "" {
		l.fail("%s est obligatoire quand %s est défini (adresse d'expédition).", VarMailFrom, VarSMTPHost)
	} else if !validEmail(s.From) {
		l.fail("%s n'est pas une adresse courriel valide.", VarMailFrom)
	}
	if (s.User == "") != (s.Password.v == "") {
		l.fail("%s et %s doivent être fournis ensemble (ou tous deux omis).", VarSMTPUser, VarSMTPPassword)
	}
}

func validEmail(a string) bool {
	at := strings.LastIndex(a, "@")
	if at < 1 || at == len(a)-1 || strings.ContainsAny(a, " \t\r\n<>,;\"") {
		return false
	}
	return validDomain(a[at+1:])
}
