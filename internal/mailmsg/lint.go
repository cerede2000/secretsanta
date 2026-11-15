package mailmsg

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Policy décrit les liens autorisés dans un message.
type Policy struct {
	// PublicHost est l'hôte (avec son port éventuel) de PUBLIC_URL : seul
	// hôte autorisé pour les liens https.
	PublicHost string
	// AllowHTTP autorise le schéma http, uniquement vers PublicHost. Le
	// constructeur ne l'active que si l'hôte de PUBLIC_URL est localhost,
	// 127.0.0.1 ou ::1 (essais en local) ; jamais sinon.
	AllowHTTP bool
	// UnsubscribeURL est le lien https de désinscription déclaré (exception
	// à la règle de l'hôte), ou "".
	UnsubscribeURL string
	// UnsubscribeMailto est l'adresse mailto: de désinscription déclarée
	// (seul mailto autorisé), ou "".
	UnsubscribeMailto string
	// Lang est la langue que doit déclarer un document HTML complet ; vide :
	// DefaultLang. Une déclaration « <Lang>-… » est acceptée.
	Lang string
}

// shorteners liste les raccourcisseurs d'adresse refusés (et leurs
// sous-domaines).
var shorteners = []string{
	"bit.ly", "bitly.com", "tinyurl.com", "t.co", "goo.gl", "ow.ly", "is.gd",
	"buff.ly", "rebrand.ly", "cutt.ly", "shorturl.at", "rb.gy", "tiny.cc",
	"lnkd.in", "s.id", "t.ly", "bl.ink", "short.io", "v.gd", "x.co", "trib.al",
	"soo.gd", "clck.ru", "qr.ae", "adf.ly", "shorte.st", "bit.do", "mcaf.ee",
	"su.pr", "po.st", "dlvr.it", "ift.tt", "tr.im", "cli.gs", "u.to", "yourls.org",
	"tny.im", "chilp.it", "shrtco.de", "rotf.lol", "linktr.ee", "surl.li", "gg.gg",
	"urlz.fr", "lc.cx", "shorturl.fr",
}

func isShortener(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	for _, s := range shorteners {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// shortenerMention repère un raccourcisseur cité sans schéma (« bit.ly/x »).
var shortenerMention = func() *regexp.Regexp {
	q := make([]string, len(shorteners))
	for i, s := range shorteners {
		q[i] = regexp.QuoteMeta(s)
	}
	return regexp.MustCompile(`(?i)(?:^|[^a-z0-9.\-])(?:[a-z0-9\-]+\.)*(?:` + strings.Join(q, "|") + `)/`)
}()

// CheckLink applique la politique à un lien : https vers l'hôte public, ou
// exactement le lien ou le mailto de désinscription déclarés ; jamais un
// raccourcisseur d'adresse.
func (p Policy) CheckLink(raw string) error {
	if hasLineBreakOrControl(raw) {
		return fail(ErrLink, "Un lien contient un caractère de contrôle.")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fail(ErrLink, fmt.Sprintf("Lien invalide : « %s ».", raw))
	}
	if u.Host != "" && isShortener(u.Hostname()) {
		return fail(ErrLink, fmt.Sprintf("Les raccourcisseurs d'adresse sont interdits : « %s ».", raw))
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "mailto" {
		if p.UnsubscribeMailto != "" && raw == p.UnsubscribeMailto {
			return nil
		}
		return fail(ErrLink, fmt.Sprintf("Seul le mailto de désinscription déclaré est autorisé : « %s ».", raw))
	}
	httpLocal := scheme == "http" && p.AllowHTTP && p.PublicHost != "" && strings.EqualFold(u.Host, p.PublicHost)
	if (scheme != "https" && !httpLocal) || u.Host == "" || u.Opaque != "" {
		return fail(ErrLink, fmt.Sprintf("Seuls les liens https sont autorisés : « %s ».", raw))
	}
	if u.User != nil {
		return fail(ErrLink, fmt.Sprintf("Un lien ne doit pas contenir d'identifiant : « %s ».", raw))
	}
	if p.UnsubscribeURL != "" && raw == p.UnsubscribeURL {
		return nil
	}
	if p.PublicHost == "" || !strings.EqualFold(u.Host, p.PublicHost) {
		return fail(ErrLink, fmt.Sprintf("Un lien doit pointer vers l'hôte de l'application (%s) : « %s ».", p.PublicHost, raw))
	}
	return nil
}

// textURL repère les liens d'un texte : schéma connu ou « www. ».
var textURL = regexp.MustCompile(`(?i)\b(?:https?|mailto|ftps?|javascript|vbscript|data|file|tel|sms|wss?):[^\s<>"«»]+|\bwww\.[^\s<>"«»]+`)

// textEmail repère une adresse courriel nue dans un texte.
var textEmail = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_{|}~\-]+@[a-z0-9\-]+(?:\.[a-z0-9\-]+)+`)

// trimURL retire la ponctuation finale qui n'appartient pas au lien.
func trimURL(s string) string {
	return strings.TrimRight(s, ".,;:!?)]}'’»")
}

// linksInText extrait les liens d'un texte brut.
func linksInText(s string) []string {
	var out []string
	for _, m := range textURL.FindAllString(s, -1) {
		out = append(out, trimURL(m))
	}
	return out
}

// LintText vérifie les liens d'une version texte (ou du texte visible d'un
// HTML) : chaque lien suit la politique, aucun lien sans schéma « www. »,
// aucun raccourcisseur même cité sans schéma.
func LintText(text string, p Policy) error {
	if shortenerMention.MatchString(text) {
		return fail(ErrLink, "Les raccourcisseurs d'adresse sont interdits.")
	}
	for _, l := range linksInText(text) {
		if strings.HasPrefix(strings.ToLower(l), "www.") {
			return fail(ErrLink, fmt.Sprintf("Un lien doit être une adresse https complète : « %s ».", l))
		}
		if err := p.CheckLink(l); err != nil {
			return err
		}
	}
	return nil
}

// voidElements n'ont pas de balise de fin.
var voidElements = map[string]bool{
	"area": true, "br": true, "col": true, "hr": true, "img": true, "meta": true,
	"wbr": true, "source": true, "track": true, "input": true,
}

// forbiddenElements chargent du contenu actif ou distant.
var forbiddenElements = map[string]string{
	"script":   "Un script est interdit dans un courriel.",
	"noscript": "Un script est interdit dans un courriel.",
	"link":     "Une feuille de style ou ressource externe est interdite.",
	"iframe":   "Un cadre (iframe) est interdit.",
	"frame":    "Un cadre est interdit.",
	"frameset": "Un cadre est interdit.",
	"object":   "Un objet embarqué est interdit.",
	"embed":    "Un objet embarqué est interdit.",
	"applet":   "Un objet embarqué est interdit.",
	"form":     "Un formulaire est interdit.",
	"input":    "Un formulaire est interdit.",
	"button":   "Un formulaire est interdit.",
	"video":    "Une vidéo est interdite.",
	"audio":    "Un son est interdit.",
	"source":   "Une ressource distante est interdite.",
	"track":    "Une ressource distante est interdite.",
	"picture":  "Une image distante est interdite.",
	"svg":      "Une image SVG est interdite.",
	"math":     "Un contenu MathML est interdit.",
	"base":     "La balise base est interdite.",
	"template": "La balise template est interdite.",
}

// cssRemote repère tout chargement depuis une feuille de style.
var cssRemote = regexp.MustCompile(`(?i)url\s*\(|@import|expression\s*\(|behavior\s*:|-moz-binding`)

// hiddenStyle repère un élément masqué ou réduit à un point.
var hiddenStyle = regexp.MustCompile(`(?i)display\s*:\s*none|visibility\s*:\s*hidden|(?:^|[;\s])(?:max-)?(?:width|height)\s*:\s*[01](?:\.0+)?(?:px)?\s*(?:;|$|!)`)

// tinySize rapporte si un attribut width/height vaut 0 ou 1 pixel.
func tinySize(v string) bool {
	v = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(v)), "px")
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return err == nil && n <= 1
}

// LintHTML contrôle un HTML (fragment de corps ou document complet) : structure
// bien formée, aucun script, gestionnaire d'événement, feuille de style
// externe, cadre ou objet ; images seulement embarquées (data:image/…) avec un
// attribut alt et jamais réduites à un pixel ; liens conformes à la politique.
// Un document complet doit déclarer la langue de Policy.Lang (« fr » si vide).
func LintHTML(src string, p Policy) error {
	z := html.NewTokenizer(strings.NewReader(src))
	var stack []string
	// Le texte visible est contrôlé deux fois : morceaux séparés (un lien
	// coupé par une balise) et morceaux accolés (un lien prolongé par une
	// balise en ligne, que le client affichera d'un seul tenant).
	var padded, joined strings.Builder
	hidden := 0 // profondeur dans head/title/style
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if len(stack) > 0 {
				return fail(ErrHTML, fmt.Sprintf("HTML invalide : la balise <%s> n'est pas fermée.", stack[len(stack)-1]))
			}
			if err := z.Err(); err != nil && !errors.Is(err, io.EOF) {
				return fail(ErrHTML, "HTML illisible.")
			}
			if err := LintText(padded.String(), p); err != nil {
				return err
			}
			return LintText(joined.String(), p)
		case html.CommentToken:
			c := strings.ToLower(string(z.Text()))
			if strings.Contains(c, "[if") || strings.Contains(c, "<") || cssRemote.MatchString(c) {
				return fail(ErrHTML, "Les commentaires conditionnels ou porteurs de balises sont interdits.")
			}
		case html.TextToken:
			if hidden == 0 {
				text := z.Text() // un seul appel : Text consomme le contenu
				padded.WriteString(" ")
				padded.Write(text)
				padded.WriteString(" ")
				joined.Write(text)
			} else if len(stack) > 0 && stack[len(stack)-1] == "style" {
				if cssRemote.Match(z.Text()) {
					return fail(ErrHTML, "Une feuille de style ne doit charger aucune ressource (url(), @import).")
				}
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			name := tok.Data
			if msg, bad := forbiddenElements[name]; bad {
				return fail(ErrHTML, msg)
			}
			if err := lintElement(name, tok.Attr, p); err != nil {
				return err
			}
			if blockElements[name] {
				joined.WriteString(" ")
			}
			if tt == html.StartTagToken && !voidElements[name] {
				stack = append(stack, name)
				if name == "head" || name == "title" || name == "style" {
					hidden++
				}
			}
		case html.EndTagToken:
			tok := z.Token()
			if voidElements[tok.Data] {
				return fail(ErrHTML, fmt.Sprintf("HTML invalide : balise de fin </%s> superflue.", tok.Data))
			}
			if len(stack) == 0 || stack[len(stack)-1] != tok.Data {
				return fail(ErrHTML, fmt.Sprintf("HTML invalide : balise de fin </%s> inattendue.", tok.Data))
			}
			stack = stack[:len(stack)-1]
			if blockElements[tok.Data] {
				joined.WriteString(" ")
			}
			if tok.Data == "head" || tok.Data == "title" || tok.Data == "style" {
				hidden--
			}
		}
	}
}

func lintElement(name string, attrs []html.Attribute, p Policy) error {
	get := func(k string) (string, bool) {
		for _, a := range attrs {
			if a.Key == k && a.Namespace == "" {
				return a.Val, true
			}
		}
		return "", false
	}
	for _, a := range attrs {
		k := strings.ToLower(a.Key)
		switch {
		case strings.HasPrefix(k, "on"):
			return fail(ErrHTML, "Les gestionnaires d'événement (on…) sont interdits.")
		case k == "style":
			if cssRemote.MatchString(a.Val) {
				return fail(ErrHTML, "Un style ne doit charger aucune ressource (url(), @import).")
			}
		case k == "background" || k == "srcset" || k == "lowsrc" || k == "dynsrc" || k == "poster" || k == "formaction" || k == "action" || k == "xlink:href" || k == "ping":
			return fail(ErrHTML, fmt.Sprintf("L'attribut %s est interdit (ressource distante).", k))
		case k == "src" && name != "img":
			return fail(ErrHTML, "Une ressource distante est interdite.")
		case k == "http-equiv":
			return fail(ErrHTML, "La balise meta http-equiv est interdite.")
		}
	}
	switch name {
	case "html":
		want := strings.ToLower(p.Lang)
		if want == "" {
			want = DefaultLang
		}
		lang, _ := get("lang")
		lang = strings.ToLower(lang)
		if lang != want && !strings.HasPrefix(lang, want+"-") {
			return fail(ErrHTML, fmt.Sprintf("Le document HTML doit déclarer lang=\"%s\".", want))
		}
	case "img":
		src, _ := get("src")
		s := strings.ToLower(strings.TrimSpace(src))
		if !strings.HasPrefix(s, "data:image/") || strings.HasPrefix(s, "data:image/svg") {
			return fail(ErrHTML, "Image distante interdite : seules les images embarquées (data:image/png, jpeg, gif, webp) sont permises.")
		}
		if _, ok := get("alt"); !ok {
			return fail(ErrHTML, "Toute image doit porter un attribut alt.")
		}
		w, wok := get("width")
		h, hok := get("height")
		style, _ := get("style")
		if (wok && tinySize(w)) || (hok && tinySize(h)) || hiddenStyle.MatchString(style) {
			return fail(ErrHTML, "Pixel de suivi interdit (image réduite à un point ou masquée).")
		}
	}
	if href, ok := get("href"); ok {
		if name != "a" && name != "area" {
			return fail(ErrHTML, fmt.Sprintf("L'attribut href est interdit sur <%s>.", name))
		}
		if err := p.CheckLink(href); err != nil {
			return err
		}
	}
	return nil
}
