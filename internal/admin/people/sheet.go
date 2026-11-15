package people

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secretsanta/internal/admin/roles"
	"secretsanta/internal/enrolment"
	"secretsanta/internal/groups"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/mail/templates"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/websec"
)

// unsubRow est l'état d'une désinscription par type de mail.
type unsubRow struct {
	LabelKey string
	Off      bool
}

// sheetView est la fiche d'une personne. Elle ne porte aucune cible.
type sheetView struct {
	ID           int64
	Name         string
	FirstName    string
	Email        string
	HasEmail     bool
	Mail         string
	MailClass    string
	MailFailed   bool
	LastSent     string
	Opened       bool
	LastLogin    string
	Revealed     bool
	Unsub        []unsubRow
	Selector     roles.Selector
	Badge        roles.Badge
	ShowBadge    bool
	ConfirmRegen bool
	NewLink      string
	QRSrc        string
	ResendURL    string
	AddressURL   string
	RegenURL     string
	EditURL      string
	RegenAsk     string
	SheetURL     string
}

type qrEntry struct {
	groupID int64
	pid     int64
	link    string
	expires time.Time
}

func (p *Pages) fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(p.o.Location).Format("2006-01-02 15:04")
}

func (p *Pages) buildSheet(ctx context.Context, g *groups.Group, pt *participants.Participant) (*view, error) {
	ft, err := loadFacts(ctx, p.o.DB, g.ID)
	if err != nil {
		return nil, err
	}
	v := p.baseView(g, "sheet")
	id := strconv.FormatInt(pt.ID, 10)
	key, class := mailChip(ft.state(pt.ID))
	sv := &sheetView{
		ID: pt.ID, Name: displayName(*pt), FirstName: pt.FirstName, Email: pt.Email,
		HasEmail: strings.TrimSpace(pt.Email) != "", Mail: key, MailClass: class,
		MailFailed: ft.state(pt.ID) == mailFailed, LastSent: p.fmtTime(ft.lastSent[pt.ID]),
		Opened: !ft.lastLogin[pt.ID].IsZero(), LastLogin: p.fmtTime(ft.lastLogin[pt.ID]),
		Revealed: ft.revealed[pt.ID], Badge: roles.NewBadge(pt.Role), ShowBadge: pt.Role != participants.RoleNormal,
		Selector:  roles.NewSelector(g.Slug, pt.ID, displayName(*pt), pt.Role, v.Base+"/"+id),
		ResendURL: v.Base + "/" + id + "/renvoyer", AddressURL: v.Base + "/" + id + "/adresse",
		RegenURL: v.Base + "/" + id + "/regenerer", EditURL: v.Base + "/" + id + "/modifier",
		RegenAsk: v.Base + "/" + id + "?regenerer=1#actions", SheetURL: v.Base + "/" + id,
	}
	for _, t := range []string{templates.UnsubRelances, templates.UnsubNouvelleIdee} {
		off, err := p.o.Unsub.IsUnsubscribed(ctx, pt.ID, t)
		if err != nil {
			return nil, errs.Internal(err)
		}
		sv.Unsub = append(sv.Unsub, unsubRow{LabelKey: "admin.people.sheet.unsub." + t, Off: off})
	}
	v.Sheet = sv
	v.BackURL = v.Base
	v.RoleHelp = roles.RoleOptions("")
	return v, nil
}

var sheetNotices = map[string]bool{"resent": true, "corrected": true, "edited": true}

// Sheet rend la fiche d'une personne (GET .../{id}).
func (p *Pages) Sheet() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		pt, err := p.person(r.Context(), g, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		v, err := p.buildSheet(r.Context(), g, pt)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		v.Sheet.ConfirmRegen = r.URL.Query().Get("regenerer") == "1"
		if k := r.URL.Query().Get("ok"); sheetNotices[k] {
			v.NoticeKey = "admin.people.notice." + k
		}
		p.render(w, r, http.StatusOK, v)
	})
}

// sendLink émet un nouveau lien et un nouveau code (les anciens cessent de
// fonctionner) et met l'invitation en file. Seule une personne avec un
// courriel peut être jointe.
func (p *Pages) sendLink(ctx context.Context, g *groups.Group, pt *participants.Participant) error {
	if strings.TrimSpace(pt.Email) == "" {
		return errs.Validation("Cette personne n'a pas de courriel.")
	}
	gid, pid := strconv.FormatInt(g.ID, 10), strconv.FormatInt(pt.ID, 10)
	tok, err := p.o.Credentials.IssueLink(ctx, gid, pid)
	if err != nil {
		return err
	}
	code, err := p.o.Credentials.IssueCode(ctx, gid, pid)
	if err != nil {
		return err
	}
	msg, err := templates.Render(p.o.Catalog, templates.Invitation, templates.Data{
		To: pt.Email, FirstName: pt.FirstName, ParticipantLang: pt.Locale, GroupName: g.Name,
		Link: p.o.LinkURL(g.Slug, tok), Code: code, BudgetCents: g.BudgetCents,
	})
	if err != nil {
		return errs.Internal(err)
	}
	_, err = p.o.Mail.EnqueueMail(ctx, queue.Item{
		GroupID: g.ID, ParticipantID: pt.ID, To: pt.Email, ToName: pt.FirstName,
		Type: mailTypeInvitation, Essential: true, Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML,
	})
	if err != nil {
		return errs.Internal(err)
	}
	return nil
}

func (p *Pages) sheetError(w http.ResponseWriter, r *http.Request, g *groups.Group, pt *participants.Participant, err error, field, fallback string) {
	v, berr := p.buildSheet(r.Context(), g, pt)
	if berr != nil {
		p.fail(w, r, berr)
		return
	}
	v.ErrorKey, v.ErrorField = errorKey(err, fallback), field
	p.render(w, r, statusOf(err), v)
}

// Resend renvoie le lien par courriel (POST .../{id}/renvoyer).
func (p *Pages) Resend() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		pt, err := p.person(r.Context(), g, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := p.sendLink(r.Context(), g, pt); err != nil {
			field := ""
			key := "admin.people.error.internal"
			if errs.Is(err, errs.KindValidation) {
				key = "admin.people.error.no_email"
			}
			v, berr := p.buildSheet(r.Context(), g, pt)
			if berr != nil {
				p.fail(w, r, berr)
				return
			}
			v.ErrorKey, v.ErrorField = key, field
			p.render(w, r, statusOf(err), v)
			return
		}
		p.journal(r.Context(), g, ActionLinkResent, id)
		http.Redirect(w, r, p.base(g)+"/"+strconv.FormatInt(id, 10)+"?ok=resent", http.StatusSeeOther)
	}))
}

// CorrectAddress corrige l'adresse puis renvoie le lien ; le changement est
// consigné au journal (POST .../{id}/adresse).
func (p *Pages) CorrectAddress() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		id, ok := pathID(r)
		if !ok || !parseForm(w, r) {
			http.NotFound(w, r)
			return
		}
		pt, err := p.person(r.Context(), g, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		email := strings.TrimSpace(r.PostForm.Get("email"))
		if email == "" {
			p.sheetError(w, r, g, pt, errs.Validation("adresse"), "email", "")
			return
		}
		updated, err := p.o.People.Update(r.Context(), participants.AdminActor(g.ID), g.ID, id, participants.Input{
			FirstName: pt.FirstName, Name: pt.Name, Email: email, GiftHint: pt.GiftHint,
		})
		if err != nil {
			p.sheetError(w, r, g, pt, err, "email", "admin.people.error.invalid")
			return
		}
		p.journal(r.Context(), g, ActionAddressFix, id)
		if err := p.sendLink(r.Context(), g, updated); err != nil {
			p.sheetError(w, r, g, updated, err, "", "admin.people.error.internal")
			return
		}
		p.journal(r.Context(), g, ActionLinkResent, id)
		http.Redirect(w, r, p.base(g)+"/"+strconv.FormatInt(id, 10)+"?ok=corrected", http.StatusSeeOther)
	}))
}

// Regenerate régénère le lien : l'ancien lien et l'ancien code cessent de
// fonctionner. Le nouveau lien et son QR code sont montrés une seule fois, dans
// la réponse ; le QR code est servi par une route de même origine
// (POST .../{id}/regenerer).
func (p *Pages) Regenerate() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		pt, err := p.person(r.Context(), g, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		gid, pid := strconv.FormatInt(g.ID, 10), strconv.FormatInt(pt.ID, 10)
		tok, err := p.o.Credentials.IssueLink(r.Context(), gid, pid)
		if err == nil {
			_, err = p.o.Credentials.IssueCode(r.Context(), gid, pid)
		}
		if err != nil {
			p.sheetError(w, r, g, pt, err, "", "admin.people.error.internal")
			return
		}
		link := p.o.LinkURL(g.Slug, tok)
		key, err := p.keepQR(g.ID, pt.ID, link)
		if err != nil {
			p.sheetError(w, r, g, pt, errs.Internal(err), "", "admin.people.error.internal")
			return
		}
		p.journal(r.Context(), g, ActionLinkRegen, id)
		v, err := p.buildSheet(r.Context(), g, pt)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		v.Sheet.NewLink = link
		v.Sheet.QRSrc = p.base(g) + "/" + pid + "/qr/" + key
		p.render(w, r, http.StatusOK, v)
	}))
}

func (p *Pages) keepQR(groupID, pid int64, link string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := hex.EncodeToString(b)
	now := p.o.Clock.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, e := range p.qrs {
		if now.After(e.expires) {
			delete(p.qrs, k)
		}
	}
	if len(p.qrs) >= maxQRs {
		for k := range p.qrs {
			delete(p.qrs, k)
			break
		}
	}
	p.qrs[key] = qrEntry{groupID: groupID, pid: pid, link: link, expires: now.Add(qrTTL)}
	return key, nil
}

// QR sert l'image PNG du lien régénéré (GET .../{id}/qr/{key}) : même origine,
// session administrateur du groupe, durée de vie courte.
func (p *Pages) QR() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		id, ok := pathID(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		p.mu.Lock()
		e, found := p.qrs[r.PathValue("key")]
		p.mu.Unlock()
		if !found || e.groupID != g.ID || e.pid != id || p.o.Clock.Now().After(e.expires) {
			http.NotFound(w, r)
			return
		}
		png, err := enrolment.QRCodePNG(e.link)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "image/png")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(png)
	})
}
