package maildkim

// Vérificateur DKIM indépendant, écrit pour les tests à partir de la RFC 6376
// (canonicalisation relaxed, §3.4.2 et §3.4.4 ; calcul de la signature, §3.7)
// et de la RFC 8463 (ed25519-sha256). Il ne partage aucun code avec le
// signataire ni avec la bibliothèque go-msgauth.

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type verified struct {
	Algorithm string
	Domain    string
	Selector  string
	Canon     string
	Headers   []string // valeur de h=, dans l'ordre
	BodyHash  string
}

var wspRun = regexp.MustCompile(`[ \t]+`)

// independentVerify vérifie la première signature DKIM du message avec la clé
// publique donnée et renvoie ses étiquettes.
func independentVerify(msg []byte, pub crypto.PublicKey) (*verified, error) {
	sep := bytes.Index(msg, []byte("\r\n\r\n"))
	if sep < 0 {
		return nil, errors.New("pas de séparation en-têtes/corps en CRLF")
	}
	headerPart := string(msg[:sep+2])
	body := msg[sep+4:]

	fields := splitFields(headerPart)
	var sigField string
	for _, f := range fields {
		if strings.EqualFold(fieldName(f), "DKIM-Signature") {
			sigField = f
			break
		}
	}
	if sigField == "" {
		return nil, errors.New("aucun en-tête DKIM-Signature")
	}
	tags, err := parseTags(fieldValue(sigField))
	if err != nil {
		return nil, err
	}
	for _, t := range []string{"v", "a", "b", "bh", "d", "h", "s"} {
		if _, ok := tags[t]; !ok {
			return nil, fmt.Errorf("étiquette %s= absente", t)
		}
	}
	if tags["v"] != "1" {
		return nil, errors.New("v= doit valoir 1")
	}
	if tags["c"] != "relaxed/relaxed" {
		return nil, fmt.Errorf("c= vaut %q, relaxed/relaxed attendu", tags["c"])
	}

	// Hachage du corps (relaxed).
	sum := sha256.Sum256(relaxedBody(body))
	if got := base64.StdEncoding.EncodeToString(sum[:]); got != stripWSP(tags["bh"]) {
		return nil, fmt.Errorf("bh= incorrect : calculé %s, annoncé %s", got, stripWSP(tags["bh"]))
	}

	// Données signées : en-têtes de h= (instances prises du bas vers le haut),
	// puis le DKIM-Signature avec b= vidé, sans CRLF final.
	var hNames []string
	for _, h := range strings.Split(tags["h"], ":") {
		hNames = append(hNames, strings.TrimSpace(stripWSP(h)))
	}
	used := make(map[int]bool)
	var data strings.Builder
	for _, name := range hNames {
		for i := len(fields) - 1; i >= 0; i-- {
			if used[i] || !strings.EqualFold(fieldName(fields[i]), name) {
				continue
			}
			used[i] = true
			data.WriteString(relaxedHeader(fields[i]))
			break
		}
	}
	data.WriteString(strings.TrimSuffix(relaxedHeader(emptyB(sigField)), "\r\n"))

	sig, err := base64.StdEncoding.DecodeString(stripWSP(tags["b"]))
	if err != nil {
		return nil, fmt.Errorf("b= illisible : %v", err)
	}
	digest := sha256.Sum256([]byte(data.String()))
	switch tags["a"] {
	case "rsa-sha256":
		k, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("a=rsa-sha256 mais clé publique non RSA")
		}
		if err := rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig); err != nil {
			return nil, fmt.Errorf("signature RSA invalide : %v", err)
		}
	case "ed25519-sha256":
		k, ok := pub.(ed25519.PublicKey)
		if !ok {
			return nil, errors.New("a=ed25519-sha256 mais clé publique non Ed25519")
		}
		if !ed25519.Verify(k, digest[:], sig) {
			return nil, errors.New("signature Ed25519 invalide")
		}
	default:
		return nil, fmt.Errorf("algorithme %q non géré", tags["a"])
	}
	return &verified{
		Algorithm: tags["a"], Domain: tags["d"], Selector: tags["s"], Canon: tags["c"],
		Headers: hNames, BodyHash: stripWSP(tags["bh"]),
	}, nil
}

// splitFields découpe la section d'en-têtes (terminée par CRLF) en champs
// complets, lignes de continuation comprises, chacun terminé par CRLF.
func splitFields(h string) []string {
	var out []string
	for _, line := range strings.SplitAfter(h, "\r\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line
			continue
		}
		out = append(out, line)
	}
	return out
}

func fieldName(f string) string {
	i := strings.IndexByte(f, ':')
	if i < 0 {
		return ""
	}
	return strings.TrimRight(f[:i], " \t")
}

func fieldValue(f string) string {
	i := strings.IndexByte(f, ':')
	return f[i+1:]
}

// relaxedHeader : nom en minuscules, dépliage, espaces réduits, espaces de
// fin et autour des deux-points supprimés, CRLF final.
func relaxedHeader(f string) string {
	name := strings.ToLower(fieldName(f))
	v := fieldValue(f)
	v = strings.ReplaceAll(v, "\r\n", "")
	v = wspRun.ReplaceAllString(v, " ")
	v = strings.Trim(v, " ")
	return name + ":" + v + "\r\n"
}

// relaxedBody : espaces réduits, espaces de fin de ligne supprimés, lignes
// vides finales supprimées, CRLF final si le corps n'est pas vide.
func relaxedBody(b []byte) []byte {
	lines := strings.Split(string(b), "\r\n")
	for i, l := range lines {
		l = wspRun.ReplaceAllString(l, " ")
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

func parseTags(v string) (map[string]string, error) {
	tags := make(map[string]string)
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(strings.ReplaceAll(part, "\r\n", ""))
		if part == "" {
			continue
		}
		k, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("étiquette mal formée %q", part)
		}
		k = strings.TrimSpace(k)
		if _, dup := tags[k]; dup {
			return nil, fmt.Errorf("étiquette %s= en double", k)
		}
		tags[k] = strings.TrimSpace(val)
	}
	return tags, nil
}

var bTag = regexp.MustCompile(`(^|[;:\s])(b[ \t]*=)[^;]*`)

// emptyB vide la valeur de l'étiquette b= (et seulement b=, pas bh=).
func emptyB(f string) string {
	name := f[:strings.IndexByte(f, ':')+1]
	v := fieldValue(f)
	v = bTag.ReplaceAllString(v, "${1}${2}")
	return name + v
}

func stripWSP(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
}
