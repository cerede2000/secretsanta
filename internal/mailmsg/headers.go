package mailmsg

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxNameLength est la longueur maximale (en caractères) d'un nom d'affichage.
const MaxNameLength = 100

// maxHeaderLine est la longueur recommandée d'une ligne d'en-tête (RFC 5322).
const maxHeaderLine = 78

// hasLineBreakOrControl rapporte la présence d'un retour à la ligne, d'un
// séparateur de ligne Unicode ou de tout autre caractère de contrôle.
func hasLineBreakOrControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || r == ' ' || r == ' ' || r == '\u0085' {
			return true
		}
	}
	return false
}

// checkField refuse un champ d'en-tête qui pourrait injecter un en-tête.
func checkField(value, field string) error {
	if !utf8.ValidString(value) {
		return fail(ErrHeaderInjection, fmt.Sprintf("%s contient des caractères invalides.", field))
	}
	if hasLineBreakOrControl(value) {
		return fail(ErrHeaderInjection, fmt.Sprintf("%s ne doit contenir ni retour à la ligne ni caractère de contrôle.", field))
	}
	return nil
}

// checkName valide un nom d'affichage et le renvoie normalisé (NFC, bords
// retirés).
func checkName(name, field string) (string, error) {
	if err := checkField(name, field); err != nil {
		return "", err
	}
	name = strings.TrimSpace(norm.NFC.String(name))
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", fail(ErrAddress, fmt.Sprintf("%s ne doit pas dépasser %d caractères.", field, MaxNameLength))
	}
	return name, nil
}

// checkAddress valide une adresse nue (addr-spec ASCII, une seule adresse,
// sans nom ni commentaire) et la renvoie.
func checkAddress(addr, field string) (string, error) {
	if err := checkField(addr, field); err != nil {
		return "", err
	}
	if addr == "" {
		return "", fail(ErrAddress, fmt.Sprintf("%s est obligatoire.", field))
	}
	for i := 0; i < len(addr); i++ {
		c := addr[i]
		if c <= ' ' || c > '~' || strings.IndexByte("<>,;:\"()[]\\", c) >= 0 {
			return "", fail(ErrAddress, fmt.Sprintf("%s n'est pas une adresse courriel valide.", field))
		}
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil || parsed.Name != "" || parsed.Address != addr || strings.Count(addr, "@") != 1 {
		return "", fail(ErrAddress, fmt.Sprintf("%s n'est pas une adresse courriel valide.", field))
	}
	d := domainOf(addr)
	if d == "" || !strings.Contains(d, ".") || strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") || strings.Contains(d, "..") {
		return "", fail(ErrAddress, fmt.Sprintf("%s n'est pas une adresse courriel valide.", field))
	}
	return addr, nil
}

// domainOf renvoie le domaine (en minuscules) d'une adresse.
func domainOf(addr string) string {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

// aligned rapporte si deux domaines sont alignés : identiques, ou l'un
// sous-domaine de l'autre.
func aligned(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

// formatAddress rend « nom <adresse> » pour l'en-tête field (nom encodé
// RFC 2047 s'il n'est pas en ASCII imprimable), ou l'adresse seule si le nom
// est vide.
func formatAddress(field, name, addr string) string {
	if name == "" {
		return addr
	}
	for i := 0; i < len(name); i++ {
		if name[i] < ' ' || name[i] > '~' {
			return encodeWords(name, len(field)+2) + " <" + addr + ">"
		}
	}
	return (&mail.Address{Name: name, Address: addr}).String()
}

// qLiteral rapporte si un octet peut figurer tel quel dans un mot encodé Q
// placé dans une « phrase » ou un texte non structuré (RFC 2047, 5.3).
func qLiteral(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
		b == '!' || b == '*' || b == '+' || b == '-' || b == '/'
}

// encodeWords encode toujours s en mots encodés RFC 2047 (UTF-8, Q), chacun
// d'au plus 75 caractères, sans couper un caractère UTF-8, séparés par un
// pli (CRLF espace). offset est la longueur déjà occupée sur la première
// ligne (« Subject: ») : aucune ligne ne dépasse 78 caractères.
func encodeWords(s string, offset int) string {
	const prefix, suffix = "=?UTF-8?Q?", "?="
	maxPayload := min(75, maxHeaderLine-offset) - len(prefix) - len(suffix)
	var words []string
	var cur strings.Builder
	for _, r := range s {
		var enc strings.Builder
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], r)
		for _, b := range buf[:n] {
			switch {
			case b == ' ':
				enc.WriteByte('_')
			case qLiteral(b):
				enc.WriteByte(b)
			default:
				fmt.Fprintf(&enc, "=%02X", b)
			}
		}
		if cur.Len()+enc.Len() > maxPayload && cur.Len() > 0 {
			words = append(words, prefix+cur.String()+suffix)
			cur.Reset()
			maxPayload = maxHeaderLine - 1 - len(prefix) - len(suffix)
		}
		cur.WriteString(enc.String())
	}
	if cur.Len() > 0 || len(words) == 0 {
		words = append(words, prefix+cur.String()+suffix)
	}
	return strings.Join(words, "\r\n ")
}

// header est un champ d'en-tête prêt à écrire.
type header struct{ name, value string }

// fold plie une ligne d'en-tête aux espaces pour rester sous 78 caractères
// quand c'est possible. Les plis imposés par la valeur (CRLF espace) sont
// conservés. Le résultat se termine par CRLF.
func fold(name, value string) string {
	var out strings.Builder
	line := name + ":"
	tokens := 0
	for i, part := range strings.Split(value, "\r\n ") {
		if i > 0 {
			out.WriteString(line + "\r\n")
			line, tokens = "", 0
		}
		for _, tok := range strings.Split(part, " ") {
			if tokens > 0 && tok != "" && len(line)+1+len(tok) > maxHeaderLine {
				out.WriteString(line + "\r\n")
				line, tokens = "", 0
			}
			line += " " + tok
			tokens++
		}
	}
	out.WriteString(line + "\r\n")
	return out.String()
}
