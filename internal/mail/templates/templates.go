// Package templates fabrique le contenu des courriels de l'application : objet,
// version texte et version HTML équivalentes, à partir des clés du catalogue
// (web/static/i18n/mail/templates/{fr,en}.json). Le résultat est un
// mailmsg.Message que le constructeur de F6b contrôle puis fabrique.
//
// Aucun modèle ne reçoit d'autre nom que celui du destinataire : l'identité du
// tireur et le nom d'une cible n'entrent jamais dans les données.
package templates

import (
	"errors"
	"fmt"
	"html"
	"strings"

	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/mailmsg"
)

// Kind identifie un modèle de courriel.
type Kind string

// Les modèles. « Tirage prêt » a trois variantes.
const (
	Invitation          Kind = "invitation"
	ReadyOne            Kind = "ready_one"
	ReadyTwo            Kind = "ready_two"
	ReadyReception      Kind = "ready_reception"
	MagicLink           Kind = "magic_link"
	LetterUpdated       Kind = "letter_updated"
	DrawAdjusted        Kind = "draw_adjusted"
	ReminderParticipant Kind = "reminder_participant"
	ReminderLetter      Kind = "reminder_letter"
	ReminderReading     Kind = "reminder_reading"
	Verification        Kind = "verification"
	Welcome             Kind = "welcome"
	BudgetUpdated       Kind = "budget_updated"
)

// Types de désinscription des modèles non essentiels.
const (
	UnsubRelances     = "relances"
	UnsubNouvelleIdee = "nouvelle_idee"
)

// DefaultAppName est le nom de l'application cité dans le pied de message.
const DefaultAppName = "Père Noël secret"

// ErrData signale des données de modèle incomplètes.
var ErrData = errors.New("templates: données incomplètes")

// Kinds liste tous les modèles, dans un ordre stable.
func Kinds() []Kind {
	return []Kind{Invitation, ReadyOne, ReadyTwo, ReadyReception, MagicLink, LetterUpdated, DrawAdjusted,
		ReminderParticipant, ReminderLetter, ReminderReading, Verification, Welcome, BudgetUpdated}
}

// ReadyKind choisit la variante de « tirage prêt » : rôle reception_seule,
// deux cibles ou une cible.
func ReadyKind(receptionOnly bool, targets int) Kind {
	switch {
	case receptionOnly:
		return ReadyReception
	case targets >= 2:
		return ReadyTwo
	default:
		return ReadyOne
	}
}

// Info décrit la nature d'un modèle.
type Info struct {
	// Essential : aucune préférence ne le coupe, pas de désinscription.
	Essential bool
	// UnsubType est le type de désinscription d'un modèle non essentiel.
	UnsubType string
}

// InfoOf renvoie la nature d'un modèle (ok faux si inconnu).
func InfoOf(k Kind) (Info, bool) {
	s, ok := specs[k]
	return s.info, ok
}

// Data regroupe les données d'un courriel. Ne contient que le destinataire et
// le groupe : jamais le tireur ni une cible.
type Data struct {
	// To est l'adresse du destinataire ; FirstName son prénom.
	To        string
	FirstName string
	// ParticipantLang et GroupLang servent à choisir la langue (voir ResolveLang).
	ParticipantLang string
	GroupLang       string
	// GroupName est le nom du groupe ; AppName celui de l'application
	// (DefaultAppName si vide).
	GroupName string
	AppName   string
	// Link est le lien personnel (ou de confirmation pour la vérification) ;
	// LetterLink le lien vers la lettre, seul lien du rôle reception_seule.
	Link       string
	LetterLink string
	// Code est le code personnel ou de vérification.
	Code string
	// BudgetCents est le budget par cadeau en centimes (nil : aucun budget).
	BudgetCents *int64
	// Ideas est le nombre d'idées de la lettre (0 : non précisé).
	Ideas int
	// UnsubscribeURL (https, signé) est obligatoire pour un modèle non
	// essentiel ; UnsubscribeMailto est facultatif.
	UnsubscribeURL    string
	UnsubscribeMailto string
}

// ResolveLang choisit la langue d'un courriel : celle du participant, sinon
// celle du groupe, sinon la langue par défaut du catalogue.
func ResolveLang(cat *catalog.Catalog, participant, group string) string {
	for _, tag := range []string{participant, group} {
		if tag == "" {
			continue
		}
		if l, ok := cat.Match(tag); ok {
			return l
		}
	}
	return cat.DefaultLang()
}

// FormatBudget écrit un montant en centimes selon la langue (« 25 € »,
// « 25,50 € » ; « €25.50 » en anglais).
func FormatBudget(lang string, cents int64) string {
	whole, frac := cents/100, cents%100
	if strings.HasPrefix(strings.ToLower(lang), "fr") {
		if frac == 0 {
			return fmt.Sprintf("%d €", whole)
		}
		return fmt.Sprintf("%d,%02d €", whole, frac)
	}
	if frac == 0 {
		return fmt.Sprintf("€%d", whole)
	}
	return fmt.Sprintf("€%d.%02d", whole, frac)
}

type linkSel int

const (
	noLink linkSel = iota
	personal
	letter
)

// block est un élément du corps : un paragraphe de catalogue, un lien, ou une
// ligne de code.
type block struct {
	key    string  // clé de paragraphe (sans le préfixe mail.tpl.)
	link   linkSel // lien affiché dans son propre paragraphe
	plural bool    // le paragraphe est une forme plurielle (tn) sur Ideas
	when   func(d *Data) bool
}

type spec struct {
	info   Info
	why    string
	blocks []block
}

func p(key string) block { return block{key: key} }

var (
	hasBudget   = func(d *Data) bool { return d.BudgetCents != nil }
	noBudget    = func(d *Data) bool { return d.BudgetCents == nil }
	hasIdeas    = func(d *Data) bool { return d.Ideas > 0 }
	hasLetterLn = func(d *Data) bool { return d.LetterLink != "" }
)

func when(b block, f func(d *Data) bool) block { b.when = f; return b }
func lnk(s linkSel) block                      { return block{link: s} }

var specs = map[Kind]spec{
	Invitation: {Info{Essential: true}, "participant", []block{
		p("invitation.p1"), p("invitation.link"), lnk(personal), p("invitation.code"),
		when(p("common.budget"), hasBudget), p("invitation.keep"),
	}},
	ReadyOne: {Info{Essential: true}, "participant", []block{
		p("ready_one.p1"), p("ready_one.link"), lnk(personal),
		when(p("common.budget"), hasBudget), p("ready_one.letter"),
	}},
	ReadyTwo: {Info{Essential: true}, "participant", []block{
		p("ready_two.p1"), p("ready_two.link"), lnk(personal),
		when(p("common.budget"), hasBudget), p("ready_two.letter"),
	}},
	ReadyReception: {Info{Essential: true}, "participant", []block{
		p("ready_reception.p1"), p("ready_reception.letter"),
		when(p("ready_reception.link"), hasLetterLn), when(lnk(letter), hasLetterLn),
		when(p("common.budget"), hasBudget),
	}},
	MagicLink: {Info{Essential: true}, "login", []block{
		p("magic_link.p1"), p("magic_link.link"), lnk(personal), p("magic_link.ignore"),
	}},
	LetterUpdated: {Info{UnsubType: UnsubNouvelleIdee}, "letter", []block{
		p("letter_updated.p1"),
		when(block{key: "letter_updated.count", plural: true}, hasIdeas),
		p("letter_updated.link"), lnk(personal),
	}},
	DrawAdjusted: {Info{Essential: true}, "participant", []block{
		p("draw_adjusted.p1"), p("draw_adjusted.link"), lnk(personal), p("draw_adjusted.old"),
	}},
	ReminderParticipant: {Info{UnsubType: UnsubRelances}, "reminder", []block{
		p("reminder_participant.p1"), p("reminder_participant.link"), lnk(personal),
	}},
	ReminderLetter: {Info{UnsubType: UnsubRelances}, "reminder", []block{
		p("reminder_letter.p1"), p("reminder_letter.link"), lnk(personal),
	}},
	ReminderReading: {Info{UnsubType: UnsubRelances}, "reminder", []block{
		p("reminder_reading.p1"), p("reminder_reading.link"), lnk(personal),
	}},
	Verification: {Info{Essential: true}, "login", []block{
		p("verification.p1"), p("verification.code"), p("verification.link"), lnk(personal), p("verification.validity"),
	}},
	Welcome: {Info{Essential: true}, "participant", []block{
		p("welcome.p1"), p("welcome.link"), lnk(personal), p("welcome.letter"),
	}},
	BudgetUpdated: {Info{Essential: true}, "participant", []block{
		p("budget_updated.p1"),
		when(p("budget_updated.amount"), hasBudget), when(p("budget_updated.none"), noBudget),
	}},
}

const prefix = "mail.tpl."

// Render compose le courriel du modèle k dans la langue du destinataire. Le
// message est prêt pour mailmsg.Builder.Build.
func Render(cat *catalog.Catalog, k Kind, d Data) (mailmsg.Message, error) {
	s, ok := specs[k]
	if !ok {
		return mailmsg.Message{}, fmt.Errorf("%w : modèle inconnu %q", ErrData, k)
	}
	if d.To == "" || strings.TrimSpace(d.FirstName) == "" || strings.TrimSpace(d.GroupName) == "" {
		return mailmsg.Message{}, fmt.Errorf("%w : destinataire, prénom et groupe sont obligatoires", ErrData)
	}
	if !s.info.Essential && d.UnsubscribeURL == "" {
		return mailmsg.Message{}, fmt.Errorf("%w : un modèle non essentiel exige un lien de désinscription", ErrData)
	}
	lang := ResolveLang(cat, d.ParticipantLang, d.GroupLang)
	app := d.AppName
	if app == "" {
		app = DefaultAppName
	}
	budget := ""
	if d.BudgetCents != nil {
		budget = FormatBudget(lang, *d.BudgetCents)
	}
	params := catalog.Params{
		"name": d.FirstName, "group": d.GroupName, "app": app,
		"code": d.Code, "budget": budget, "n": d.Ideas,
	}

	var text, htm strings.Builder
	para := func(s string) {
		text.WriteString(s + "\n\n")
		htm.WriteString("<p>" + html.EscapeString(s) + "</p>\n")
	}
	link := func(u string) {
		text.WriteString(u + "\n\n")
		e := html.EscapeString(u)
		htm.WriteString(`<p><a href="` + e + `">` + e + "</a></p>\n")
	}

	para(cat.T(lang, prefix+"common.greeting", params))
	for _, b := range s.blocks {
		if b.when != nil && !b.when(&d) {
			continue
		}
		switch {
		case b.link != noLink:
			u := d.Link
			if b.link == letter {
				u = d.LetterLink
			}
			if u == "" {
				return mailmsg.Message{}, fmt.Errorf("%w : lien manquant pour %q", ErrData, k)
			}
			link(u)
		case b.plural:
			para(cat.TN(lang, prefix+b.key, d.Ideas, params))
		default:
			para(cat.T(lang, prefix+b.key, params))
		}
	}
	if (k == Invitation || k == Verification) && d.Code == "" {
		return mailmsg.Message{}, fmt.Errorf("%w : code manquant pour %q", ErrData, k)
	}

	// Pied de message : pourquoi la personne reçoit ce courriel, qui l'envoie.
	text.WriteString("--\n")
	htm.WriteString("<hr>\n")
	para(cat.T(lang, prefix+"footer.why."+s.why, params))
	para(cat.T(lang, prefix+"footer.sender", params))
	if !s.info.Essential {
		para(cat.T(lang, prefix+"footer.unsubscribe."+s.info.UnsubType, params))
		link(d.UnsubscribeURL)
	}

	m := mailmsg.Message{
		To:        d.To,
		ToName:    d.FirstName,
		Subject:   cat.T(lang, prefix+string(k)+".subject", params),
		Text:      strings.TrimRight(text.String(), "\n") + "\n",
		HTML:      strings.TrimRight(htm.String(), "\n"),
		Essential: s.info.Essential,
	}
	if !s.info.Essential {
		m.UnsubscribeURL = d.UnsubscribeURL
		m.UnsubscribeMailto = d.UnsubscribeMailto
	}
	return m, nil
}
