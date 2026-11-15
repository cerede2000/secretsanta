package queue

import (
	"context"
	"html"
	"regexp"
	"strings"

	"secretsanta/internal/shared/ports"
)

// Sender implémente le port de courriel de F1 au-dessus de la file. Le port ne
// porte ni groupe, ni nom, ni version HTML : le HTML est dérivé du texte et les
// modules qui connaissent ces données appellent EnqueueMail directement.
type Sender struct{ S *Service }

var _ ports.EmailSender = Sender{}

// Send met le message en file (il part au prochain Tick).
func (p Sender) Send(ctx context.Context, msg ports.EmailMessage) error {
	_, err := p.S.EnqueueMail(ctx, Item{
		To: msg.To, Type: string(msg.Type), Essential: msg.Essential,
		Subject: msg.Subject, Text: msg.Body, HTML: HTMLFromText(msg.Body),
		CoalesceKey: msg.GroupingKey,
	})
	return err
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"]+`)

// HTMLFromText dérive une version HTML équivalente d'un texte brut : paragraphes,
// retours à la ligne et adresses https rendues cliquables.
func HTMLFromText(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	var out strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		var lines []string
		for _, l := range strings.Split(para, "\n") {
			lines = append(lines, linkify(l))
		}
		out.WriteString("<p>" + strings.Join(lines, "<br>") + "</p>\n")
	}
	return out.String()
}

func linkify(line string) string {
	var b strings.Builder
	last := 0
	for _, loc := range urlPattern.FindAllStringIndex(line, -1) {
		u := line[loc[0]:loc[1]]
		b.WriteString(html.EscapeString(line[last:loc[0]]))
		e := html.EscapeString(u)
		b.WriteString(`<a href="` + e + `">` + e + `</a>`)
		last = loc[1]
	}
	b.WriteString(html.EscapeString(line[last:]))
	return b.String()
}
