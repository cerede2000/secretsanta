package config

import (
	"os"
	"strings"
)

var headerNameOK = func(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func (l *loader) loadDKIM(c *Config) {
	d := &c.DKIM
	// Une clé privée en clair dans l'environnement est refusée, quelle que soit sa forme.
	if strings.TrimSpace(l.getenv(varPrivateKeyInEnv)) != "" {
		l.fail("%s : la clé privée ne doit jamais être fournie en clair dans une variable ; la placer dans un fichier et indiquer son chemin avec %s.", varPrivateKeyInEnv, VarMailDKIMKeyFile)
	}
	domain := l.get(VarMailDKIMDomain)
	selector := l.get(VarMailDKIMSelector)
	keyFile := l.get(VarMailDKIMKeyFile)
	alg := strings.ToLower(l.get(VarMailDKIMAlgorithm))
	headers := l.get(VarMailDKIMHeaders)

	if strings.Contains(keyFile, "PRIVATE KEY") || strings.Contains(keyFile, "-----") || strings.ContainsAny(keyFile, "\r\n") {
		l.fail("%s doit contenir le chemin d'un fichier, jamais la clé privée elle-même.", VarMailDKIMKeyFile)
		return
	}

	if domain == "" && selector == "" && keyFile == "" && alg == "" {
		return // signature désactivée
	}
	complete := true
	for _, r := range []struct{ name, val string }{
		{VarMailDKIMDomain, domain}, {VarMailDKIMSelector, selector},
		{VarMailDKIMKeyFile, keyFile}, {VarMailDKIMAlgorithm, alg},
	} {
		if r.val == "" {
			l.fail("Configuration DKIM incomplète : %s est obligatoire dès qu'une variable DKIM est définie.", r.name)
			complete = false
		}
	}
	if !complete {
		return
	}
	if !validDomain(domain) {
		l.fail("%s doit être un nom de domaine valide.", VarMailDKIMDomain)
	}
	if !validSelector(selector) {
		l.fail("%s n'est pas un sélecteur DKIM valide.", VarMailDKIMSelector)
	}
	switch alg {
	case AlgRSA, AlgEd25519:
	default:
		l.fail("%s doit valoir rsa-sha256 ou ed25519-sha256.", VarMailDKIMAlgorithm)
	}
	if b, err := os.ReadFile(keyFile); err != nil {
		l.fail("%s : le fichier de clé privée est introuvable ou illisible.", VarMailDKIMKeyFile)
	} else if !strings.Contains(string(b), "PRIVATE KEY") {
		l.fail("%s : le fichier ne contient pas de clé privée au format PEM.", VarMailDKIMKeyFile)
	}

	hs := DefaultDKIMHeaders
	if headers != "" {
		hs = headers
	}
	var list []string
	for _, h := range strings.Split(hs, ",") {
		h = strings.TrimSpace(h)
		if !headerNameOK(h) {
			l.fail("%s doit être une liste de noms d'en-têtes séparés par des virgules.", VarMailDKIMHeaders)
			list = nil
			break
		}
		list = append(list, h)
	}
	hasFrom := false
	for _, h := range list {
		hasFrom = hasFrom || strings.EqualFold(h, "From")
	}
	if list != nil && !hasFrom {
		l.fail("%s doit inclure l'en-tête From.", VarMailDKIMHeaders)
	}
	d.Domain, d.Selector, d.KeyFile, d.Algorithm, d.Headers = domain, selector, NewSecret(keyFile), alg, list
}

// validSelector accepte les étiquettes DNS (points autorisés).
func validSelector(s string) bool {
	for _, p := range strings.Split(s, ".") {
		if !headerNameOK(p) {
			return false
		}
	}
	return true
}
