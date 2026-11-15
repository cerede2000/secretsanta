package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/groups"
	"secretsanta/internal/groupsettings"
	"secretsanta/internal/i18n/catalog"
	i18nweb "secretsanta/internal/i18n/web"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/ports"
	"secretsanta/internal/websec"
)

// ErrMailDisabled est renvoyée quand aucun transport d'envoi n'est branché :
// le message n'est pas mis en file (il partirait périmé, ou jamais).
var ErrMailDisabled = queue.ErrMailDisabled

// mailOutbox est l'expéditeur donné aux fonctionnalités : la file d'envoi
// quand elle tourne, sinon un refus net (ErrMailDisabled).
type mailOutbox struct{ q *queue.Service }

// Send met le message en file, ou refuse si l'envoi est désactivé.
func (m mailOutbox) Send(ctx context.Context, msg ports.EmailMessage) error {
	if m.q == nil {
		return ErrMailDisabled
	}
	return queue.Sender{S: m.q}.Send(ctx, msg)
}

var _ ports.EmailSender = mailOutbox{}

// mailDisabled dit si l'envoi de courriel est inactif (aucun transport) : les
// pages d'administration en avertissent l'organisateur.
func (a *App) mailDisabled() bool { return a.mail.q == nil }

// participantDirectory adapte participants.Service au port rules.Directory
// (lecture du rôle de tirage). Aucun lecteur dédié n'existe dans participants :
// l'adaptateur passe par Get avec l'acteur administrateur du groupe, ce qui
// reste borné au groupe demandé.
type participantDirectory struct{ svc *participants.Service }

// DrawRole renvoie le rôle ; ok est faux si le participant n'existe pas dans
// le groupe.
func (d participantDirectory) DrawRole(ctx context.Context, groupID, participantID int64) (string, bool, error) {
	p, err := d.svc.Get(ctx, participants.AdminActor(groupID), groupID, participantID)
	if err != nil {
		var e *errs.Error
		if errors.As(err, &e) && e.Kind == errs.KindNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return string(p.Role), true, nil
}

// participantEmails retrouve un participant par son adresse, au-dessus de
// participants (qui n'offre pas de recherche par adresse) : liste du groupe
// avec l'acteur administrateur, comparaison sans casse ni espaces. Si deux
// participants partagent l'adresse, le plus ancien (identifiant le plus petit)
// est retenu.
type participantEmails struct{ svc *participants.Service }

func (d participantEmails) find(ctx context.Context, groupID int64, email string) (*participants.Participant, error) {
	if groupID <= 0 {
		return nil, nil
	}
	list, err := d.svc.List(ctx, participants.AdminActor(groupID), groupID)
	if err != nil {
		return nil, err
	}
	want := strings.TrimSpace(email)
	var best *participants.Participant
	for i := range list {
		p := &list[i]
		if !strings.EqualFold(strings.TrimSpace(p.Email), want) {
			continue
		}
		if best == nil || p.ID < best.ID {
			best = p
		}
	}
	return best, nil
}

// FindByEmail implémente magic.Directory.
func (d participantEmails) FindByEmail(ctx context.Context, groupID int64, email string) (int64, bool, error) {
	p, err := d.find(ctx, groupID, email)
	if err != nil || p == nil {
		return 0, false, err
	}
	return p.ID, true, nil
}

// magicComposer fabrique le courriel du lien magique avec le modèle MagicLink.
// Le composeur du paquet magic ne reçoit que l'adresse et le lien : le groupe
// se lit dans le lien (premier segment du chemin), le prénom et la langue du
// participant dans la base.
type magicComposer struct {
	cat      *catalog.Catalog
	groups   *groups.Service
	settings *groupsettings.Service
	people   participantEmails
	base     string
}

const composeTimeout = 10 * time.Second

func (c magicComposer) compose(to, link string) ports.EmailMessage {
	ctx, cancel := context.WithTimeout(context.Background(), composeTimeout)
	defer cancel()
	slug := slugOfLink(link)
	d := templates.Data{To: to, FirstName: localPart(to), GroupName: slug, Link: link}
	if g, err := c.groups.BySlug(ctx, slug); err == nil && g != nil {
		d.GroupName = g.Name
		if p, err := c.people.find(ctx, g.ID, to); err == nil && p != nil {
			if strings.TrimSpace(p.FirstName) != "" {
				d.FirstName = p.FirstName
			}
			d.ParticipantLang = p.Locale
		}
		if l, ok := c.settings.GroupLanguage(ctx, slug); ok {
			d.GroupLang = l
		}
	}
	m, err := templates.Render(c.cat, templates.MagicLink, d)
	if err != nil {
		lang := c.cat.DefaultLang()
		return ports.EmailMessage{To: to, Subject: c.cat.T(lang, "mail.tpl.magic_link.subject", nil), Body: link + "\n"}
	}
	return ports.EmailMessage{To: to, Subject: m.Subject, Body: m.Text}
}

// slugOfLink renvoie le premier segment du chemin d'un lien.
func slugOfLink(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	seg, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	return seg
}

// localPart renvoie la partie locale d'une adresse (prénom de repli).
func localPart(addr string) string {
	if i := strings.IndexByte(addr, '@'); i > 0 {
		return addr[:i]
	}
	return addr
}

// participantLocale adapte participants.Service au port
// negotiate.ParticipantLocale : le changement de langue (POST /langue) est
// enregistré pour le participant connecté. Le groupe se lit dans le champ
// « next » du formulaire (chemin local, premier segment = adresse du groupe),
// puis la session participant de ce groupe désigne la personne ; sans session
// de ce groupe, rien n'est enregistré.
type participantLocale struct {
	groups *groups.Service
	parts  *participants.Service
}

// RememberLocale enregistre la langue ; recorded est faux sans session.
func (p participantLocale) RememberLocale(ctx context.Context, r *http.Request, lang string) (bool, error) {
	next := i18nweb.SafeNext(r.PostFormValue("next"))
	u, err := url.Parse(next)
	if err != nil {
		return false, nil
	}
	slug, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if slug == "" {
		return false, nil
	}
	g, err := p.groups.BySlug(ctx, slug)
	if err != nil {
		if errs.Is(err, errs.KindNotFound) {
			return false, nil
		}
		return false, err
	}
	gid := strconv.FormatInt(g.ID, 10)
	sess, ok := websec.ParticipantSession(ctx, gid)
	if !ok {
		return false, nil
	}
	pid, err := strconv.ParseInt(sess.Subject, 10, 64)
	if err != nil {
		return false, nil
	}
	if err := p.parts.SetLocale(ctx, participants.ParticipantActor(g.ID, pid), g.ID, pid, lang); err != nil {
		return false, err
	}
	return true, nil
}
