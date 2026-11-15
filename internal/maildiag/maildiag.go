// Package maildiag diagnostique la délivrabilité du courrier : SPF, DKIM,
// DMARC, MX, alignement, TLS du serveur SMTP et HTTPS de l'adresse publique.
//
// Toutes les entrées viennent de la configuration (jamais d'un hôte saisi),
// aucune donnée personnelle n'est émise et les requêtes HTTP passent par le
// client sortant de F1b. Le rapport ne contient pas de texte : chaque point
// porte un Code stable (« spf.missing ») que la page traduit par les
// catalogues, plus des paramètres et, au besoin, l'enregistrement DNS à copier.
package maildiag

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/clock"
	"secretsanta/internal/shared/outbound"
)

// Status est la gravité d'un point du rapport.
type Status string

const (
	StatusOK    Status = "ok"    // correct
	StatusWarn  Status = "warn"  // à corriger
	StatusError Status = "error" // erreur
	StatusOff   Status = "off"   // non activé / sans objet
)

// Identifiants des points du rapport.
const (
	CheckSPF          = "spf"
	CheckDKIM         = "dkim"
	CheckDMARC        = "dmarc"
	CheckMX           = "mx"
	CheckAlignEnv     = "alignment_envelope"
	CheckAlignDKIM    = "alignment_dkim"
	CheckSMTPTLS      = "smtp_tls"
	CheckPublicHTTPS  = "public_https"
	CheckPublicHealth = "public_health"
)

// GuideAnchor est le renvoi vers le guide de délivrabilité du README (F12).
const GuideAnchor = "README.md#délivrabilité"

// Record est un enregistrement DNS à copier.
type Record struct {
	Name  string
	Type  string
	Value string
}

// Check est un point du rapport.
type Check struct {
	ID     string
	Status Status
	// Code identifie le message à afficher : "<ID>.<variante>".
	Code   string
	Params map[string]string
	// Record est l'enregistrement proposé (nil quand rien à corriger).
	Record *Record
}

// NotVerifiable est un point que l'application ne peut pas contrôler.
type NotVerifiable struct {
	ID    string // ip_reputation, blocklists, reverse_dns
	Guide string // renvoi au guide de délivrabilité
}

// Report est le résultat complet du diagnostic.
type Report struct {
	Domain        string
	Checks        []Check
	NotVerifiable []NotVerifiable
}

// Check renvoie le point d'identifiant id.
func (r Report) Check(id string) (Check, bool) {
	for _, c := range r.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

// Options règle le diagnostic.
type Options struct {
	Config   *config.Config
	Resolver Resolver     // défaut : résolveur du système
	Clock    clock.Clock  // défaut : horloge réelle
	Client   *http.Client // défaut : outbound.New
	// TLSConfig remplace la configuration TLS (tests : autorité de confiance).
	TLSConfig *tls.Config
	// Timeout borne chaque connexion (10 s par défaut).
	Timeout time.Duration
}

// Diagnostic exécute les contrôles.
type Diagnostic struct {
	cfg     *config.Config
	res     Resolver
	clk     clock.Clock
	client  *http.Client
	tlsCfg  *tls.Config
	timeout time.Duration
}

// New construit le diagnostic.
func New(o Options) *Diagnostic {
	d := &Diagnostic{cfg: o.Config, res: o.Resolver, clk: o.Clock, client: o.Client, tlsCfg: o.TLSConfig, timeout: o.Timeout}
	if d.res == nil {
		d.res = NetResolver{}
	}
	if d.clk == nil {
		d.clk = clock.Real{}
	}
	if d.timeout <= 0 {
		d.timeout = 10 * time.Second
	}
	if d.client == nil {
		d.client = outbound.New(outbound.Options{Timeout: d.timeout})
	}
	return d
}

// Run exécute tous les contrôles.
func (d *Diagnostic) Run(ctx context.Context) Report {
	rep := Report{NotVerifiable: NotVerifiableItems()}
	if d.cfg.SMTP.Enabled() {
		dom := addrDomain(d.cfg.SMTP.From)
		rep.Domain = dom
		rep.Checks = append(rep.Checks,
			d.checkSPF(ctx, dom),
			d.checkDKIM(ctx),
			d.checkDMARC(ctx, dom),
			d.checkMX(ctx, dom),
		)
		rep.Checks = append(rep.Checks, d.checkAlignment()...)
		rep.Checks = append(rep.Checks, d.checkSMTPTLS(ctx))
	} else {
		rep.Checks = append(rep.Checks, mk("mail", StatusOff, "disabled", nil, nil))
	}
	rep.Checks = append(rep.Checks, d.checkPublicURL(ctx)...)
	return rep
}

func mk(id string, st Status, variant string, params map[string]string, rec *Record) Check {
	return Check{ID: id, Status: st, Code: id + "." + variant, Params: params, Record: rec}
}

// addrDomain renvoie le domaine (en minuscules) d'une adresse courriel.
func addrDomain(addr string) string {
	if a, err := mail.ParseAddress(addr); err == nil {
		addr = a.Address
	}
	i := strings.LastIndexByte(addr, '@')
	if i < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr[i+1:]))
}
