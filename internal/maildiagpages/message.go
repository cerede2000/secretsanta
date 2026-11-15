package maildiagpages

import (
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mailmsg"
)

// messageOf reproduit le message que la file fabrique pour un élément.
func messageOf(it queue.Item) mailmsg.Message {
	return mailmsg.Message{To: it.To, ToName: it.ToName, Subject: it.Subject, Text: it.Text, HTML: it.HTML,
		Essential: it.Essential, UnsubscribeURL: it.UnsubscribeURL}
}
