package app

import (
	"context"
	"fmt"

	"secretsanta/internal/admin/adjustpages"
	"secretsanta/internal/admin/settingspages"
	"secretsanta/internal/reminders"
	"secretsanta/internal/shared/errs"
)

// noReminders tient la place du moteur de relances quand aucune file d'envoi ne
// tourne (aucun transport) : la relance manuelle est refusée proprement.
type noReminders struct{}

// Remind implémente settingspages.Reminders.
func (noReminders) Remind(context.Context, int64, reminders.Kind, []int64) (reminders.ManualResult, error) {
	return reminders.ManualResult{}, errs.Validation("L'envoi de courriels n'est pas configuré.")
}

var _ settingspages.Reminders = noReminders{}

// registerAdjustPages monte « Ajuster le tirage » : GET et POST
// /{slug}/admin/ajuster sur Slugs. L'ajustement passe par Adjusting, qui met
// déjà la révélation à jour (et la retente) : OnDrawChanged reste vide pour ne
// pas doubler le travail. Elle suit registerAdminPages (navigation, journal).
func registerAdjustPages(a *App) error {
	p, err := adjustpages.NewPages(adjustpages.Options{
		Groups: a.Groups, Adjust: a.Adjust, People: a.Participants,
		Membership: adjustpages.SQLMembership{DB: a.DB},
		Translator: a.Cat, Audit: a.Audit, NavFor: a.adminNav,
	})
	if err != nil {
		return fmt.Errorf("page d'ajustement : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}

// registerSettingsPages monte les réglages du groupe et les relances manuelles :
// GET /{slug}/admin/reglages, POST .../reglages/groupe, .../reglages/relances,
// .../reglages/courrier/test, GET et POST /{slug}/admin/relances. Les routes de
// la section Données (export, import, sauvegarde, duplication, suppression) sont
// celles d'adminops (registerAdminOps). Sans file d'envoi, le courriel de test
// et les relances sont refusés avec un message. Elle suit registerAdminOps.
func registerSettingsPages(a *App) error {
	o := settingspages.Options{
		Groups: a.Groups, Settings: a.GroupSettings, Reveal: a.Reveal, Rhythm: a.ReminderRules,
		Reminders: noReminders{}, People: a.Participants, Ops: a.Ops,
		Translator: a.Cat, Audit: a.Audit,
		SMTP: a.Cfg.SMTP, Delivery: a.Cfg.Mail, DKIM: a.Cfg.DKIM,
		Location: a.Cfg.Location, NavFor: a.adminNav,
	}
	if a.Reminders != nil {
		o.Reminders = a.Reminders
	}
	if a.MailQueue != nil && a.mail.q != nil {
		o.Mail = a.MailQueue
	}
	p, err := settingspages.NewPages(o)
	if err != nil {
		return fmt.Errorf("page des réglages : %w", err)
	}
	p.Register(a.Slugs)
	return nil
}
