package people

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"secretsanta/internal/admin/roles"
	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/websec"
)

// Valeurs du filtre « état d'accès ».
const (
	accessSent    = "envoye"
	accessFailed  = "echec"
	accessPending = "en_attente"
	accessNoMail  = "aucun"
	accessOpened  = "ouvert"
	accessNever   = "jamais"
)

var accessFilters = []string{accessSent, accessFailed, accessPending, accessNoMail, accessOpened, accessNever}

// rowView est une ligne de la liste.
type rowView struct {
	ID          int64
	Name        string
	Email       string
	HasEmail    bool
	SharedEmail bool
	ShowBadge   bool
	Badge       roles.Badge
	Mail        string // clé de texte de l'état du courriel
	MailClass   string
	Opened      bool
	Selector    roles.Selector
	Confirm     bool
	Href        string
	EditHref    string
	ResendHref  string
	DeleteHref  string
	DeleteURL   string
}

type sectionView struct {
	HouseholdID int64
	Name        string
	Rows        []rowView
}

type optionView struct {
	Value    string
	Label    string
	LabelKey string
	Selected bool
}

type householdView struct {
	ID      int64
	Name    string
	Members []memberView
}

type memberView struct {
	ID   int64
	Name string
}

type exclusionView struct {
	A, B       string
	AID, BID   int64
	Both       bool
	RemoveBoth bool
}

// confirmRule est l'aperçu en comptes d'un changement de règle après tirage.
type confirmRule struct {
	Fields     []field
	Affected   int
	Discovered int
	Explain    string
}

type field struct{ Name, Value string }

// view est la donnée de toutes les pages du module.
type view struct {
	Lang, TitleKey string
	Nav            []adminnav.Item
	Page           string // list, edit, sheet
	Slug           string
	GroupName      string
	Base           string
	BackURL        string

	// Liste.
	Query         string
	AccessSel     string
	HouseholdSel  string
	RoleSel       string
	NoEmailSel    bool
	AccessOpts    []optionView
	HouseholdOpts []optionView
	RoleOpts      []roles.RoleOption
	Total         int
	Shown         int
	Sections      []sectionView
	DrawDone      bool
	RoleHelp      []roles.RoleOption
	Households    []householdView
	Exclusions    []exclusionView
	ForcedCount   int
	People        []optionView
	RuleConfirm   *confirmRule

	// Formulaires.
	Form        formValues
	ErrorKey    string
	ErrorField  string
	NoticeKey   string
	DeleteName  string
	DeleteID    int64
	DeleteError bool

	Sheet *sheetView

	// Kinds donne aux gabarits les valeurs du champ « kind » des règles.
	Kinds ruleKinds
}

type ruleKinds struct{ Exclusion, Forced, Household, Member string }

type formValues struct {
	FirstName, Name, Email, GiftHint string
	Action, SubmitKey, TitleKey      string
	ID                               int64
}

// listFilter est la lecture du formulaire de recherche.
type listFilter struct {
	q, access, household, role string
	noEmail                    bool
}

func readFilter(q url.Values) listFilter {
	f := listFilter{q: strings.TrimSpace(q.Get("q")), household: q.Get("foyer"), noEmail: q.Get("sans_courriel") == "1"}
	if len(f.q) > 120 {
		f.q = f.q[:120]
	}
	for _, a := range accessFilters {
		if q.Get("acces") == a {
			f.access = a
		}
	}
	if _, ok := roles.ParseRole(q.Get("role")); ok {
		f.role = q.Get("role")
	}
	return f
}

func (f listFilter) match(pt participants.Participant, ft *facts, hh map[int64]int64) bool {
	if f.q != "" {
		hay := strings.ToLower(displayName(pt) + " " + pt.Email)
		if !strings.Contains(hay, strings.ToLower(f.q)) {
			return false
		}
	}
	has := strings.TrimSpace(pt.Email) != ""
	if f.noEmail && has {
		return false
	}
	if f.role != "" && string(pt.Role) != f.role {
		return false
	}
	switch f.household {
	case "":
	case "aucun":
		if _, in := hh[pt.ID]; in {
			return false
		}
	default:
		id, err := strconv.ParseInt(f.household, 10, 64)
		if err != nil || hh[pt.ID] != id {
			return false
		}
	}
	_, opened := ft.lastLogin[pt.ID]
	st := ft.state(pt.ID)
	switch f.access {
	case accessSent:
		return st == mailSent
	case accessFailed:
		return st == mailFailed
	case accessPending:
		return st == mailPending
	case accessNoMail:
		return !has
	case accessOpened:
		return opened
	case accessNever:
		return !opened
	}
	return true
}

func mailChip(st mailState) (key, class string) {
	switch st {
	case mailSent:
		return "admin.people.mail.sent", "chip-ok"
	case mailFailed:
		return "admin.people.mail.failed", "chip-error"
	case mailPending:
		return "admin.people.mail.pending", "chip-warn"
	}
	return "admin.people.mail.none", ""
}

func (p *Pages) baseView(g *groups.Group, page string) *view {
	return &view{Page: page, Slug: g.Slug, GroupName: g.Name, Base: p.base(g), BackURL: "/" + g.Slug + "/admin", TitleKey: "admin.people.title",
		Kinds: ruleKinds{Exclusion: kindExclusion, Forced: kindForced, Household: kindHousehold, Member: kindMember}}
}

// buildList prépare la page des participants pour la requête.
func (p *Pages) buildList(ctx context.Context, g *groups.Group, q url.Values) (*view, error) {
	a := participants.AdminActor(g.ID)
	list, err := p.o.People.List(ctx, a, g.ID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(displayName(list[i])) < strings.ToLower(displayName(list[j]))
	})
	ft, err := loadFacts(ctx, p.o.DB, g.ID)
	if err != nil {
		return nil, err
	}
	hhs, err := loadHouseholds(ctx, p.o.DB, g.ID)
	if err != nil {
		return nil, err
	}
	excl, forced, err := loadExclusions(ctx, p.o.DB, g.ID)
	if err != nil {
		return nil, err
	}
	done, err := p.o.State.IsDrawDone(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	f := readFilter(q)
	v := p.baseView(g, "list")
	v.Query, v.AccessSel, v.HouseholdSel, v.RoleSel, v.NoEmailSel = f.q, f.access, f.household, f.role, f.noEmail
	v.DrawDone, v.Total, v.ForcedCount = done, len(list), forced
	v.RoleOpts, v.RoleHelp = roles.RoleOptions(f.role), roles.RoleOptions("")
	v.Form = formValues{Action: v.Base, SubmitKey: "admin.people.add.submit"}
	for _, ac := range accessFilters {
		v.AccessOpts = append(v.AccessOpts, optionView{Value: ac, LabelKey: "admin.people.filter.access." + ac, Selected: ac == f.access})
	}

	names := map[int64]string{}
	emailUse := map[string]int{}
	for _, pt := range list {
		names[pt.ID] = displayName(pt)
		v.People = append(v.People, optionView{Value: strconv.FormatInt(pt.ID, 10), Label: displayName(pt)})
		if e := strings.ToLower(strings.TrimSpace(pt.Email)); e != "" {
			emailUse[e]++
		}
	}
	hh := map[int64]int64{}
	for _, h := range hhs {
		hv := householdView{ID: h.ID, Name: h.Name}
		for _, m := range h.Members {
			hh[m] = h.ID
			if n, ok := names[m]; ok {
				hv.Members = append(hv.Members, memberView{ID: m, Name: n})
			}
		}
		v.Households = append(v.Households, hv)
		v.HouseholdOpts = append(v.HouseholdOpts, optionView{Value: strconv.FormatInt(h.ID, 10), Label: h.Name, Selected: strconv.FormatInt(h.ID, 10) == f.household})
	}
	for _, e := range excl {
		an, aok := names[e.A]
		bn, bok := names[e.B]
		if aok && bok {
			v.Exclusions = append(v.Exclusions, exclusionView{A: an, B: bn, AID: e.A, BID: e.B, Both: e.Both})
		}
	}

	byHousehold := map[int64]*sectionView{}
	var order []int64
	var loose sectionView
	for _, pt := range list {
		if !f.match(pt, ft, hh) {
			continue
		}
		v.Shown++
		key, class := mailChip(ft.state(pt.ID))
		id := strconv.FormatInt(pt.ID, 10)
		row := rowView{
			ID: pt.ID, Name: displayName(pt), Email: pt.Email, HasEmail: strings.TrimSpace(pt.Email) != "",
			SharedEmail: emailUse[strings.ToLower(strings.TrimSpace(pt.Email))] > 1 && strings.TrimSpace(pt.Email) != "",
			ShowBadge:   pt.Role != participants.RoleNormal, Badge: roles.NewBadge(pt.Role),
			Mail: key, MailClass: class, Opened: !ft.lastLogin[pt.ID].IsZero(),
			Selector: roles.NewSelector(g.Slug, pt.ID, displayName(pt), pt.Role, v.Base),
			Confirm:  false,
			Href:     v.Base + "/" + id, EditHref: v.Base + "/" + id + "/modifier",
			ResendHref: v.Base + "/" + id + "/renvoyer", DeleteHref: v.Base + "?supprimer=" + id + "#p-" + id,
			DeleteURL: v.Base + "/" + id + "/supprimer",
		}
		if hid, in := hh[pt.ID]; in {
			s := byHousehold[hid]
			if s == nil {
				s = &sectionView{HouseholdID: hid}
				for _, h := range hhs {
					if h.ID == hid {
						s.Name = h.Name
					}
				}
				byHousehold[hid] = s
				order = append(order, hid)
			}
			s.Rows = append(s.Rows, row)
		} else {
			loose.Rows = append(loose.Rows, row)
		}
	}
	for _, hid := range order {
		v.Sections = append(v.Sections, *byHousehold[hid])
	}
	if len(loose.Rows) > 0 {
		v.Sections = append(v.Sections, loose)
	}
	// Confirmation de suppression intégrée (pas de boîte du navigateur).
	if raw := q.Get("supprimer"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if n, ok := names[id]; ok {
				v.DeleteID, v.DeleteName = id, n
			}
		}
	}
	if k := q.Get("ok"); noticeKnown(k) {
		v.NoticeKey = "admin.people.notice." + k
	}
	return v, nil
}

var notices = map[string]bool{"added": true, "edited": true, "deleted": true, "rule": true, "removed": true}

func noticeKnown(k string) bool { return notices[k] }

// List rend la page des participants (GET /{slug}/admin/participants).
func (p *Pages) List() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		v, err := p.buildList(r.Context(), g, r.URL.Query())
		if err != nil {
			p.fail(w, r, err)
			return
		}
		p.render(w, r, http.StatusOK, v)
	})
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, "Internal error", statusOf(err))
}

func parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	return r.ParseForm() == nil
}

func inputFrom(r *http.Request) participants.Input {
	return participants.Input{
		FirstName: r.PostForm.Get("first_name"), Name: r.PostForm.Get("name"),
		Email: r.PostForm.Get("email"), GiftHint: r.PostForm.Get("gift_hint"),
	}
}

func formOf(in participants.Input) formValues {
	return formValues{FirstName: in.FirstName, Name: in.Name, Email: in.Email, GiftHint: in.GiftHint}
}

// fieldOf devine le champ fautif d'une erreur de saisie.
func fieldOf(err error, in participants.Input) string {
	if errsKind(err) == "conflict" {
		return "name"
	}
	if strings.TrimSpace(in.FirstName) == "" {
		return "first_name"
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name"
	}
	return "email"
}

// Create ajoute une personne (POST /{slug}/admin/participants).
func (p *Pages) Create() http.Handler {
	return websec.RequireUnsafeMethod(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := p.admin(w, r)
		if !ok {
			return
		}
		if !parseForm(w, r) {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		in := inputFrom(r)
		pt, err := p.o.People.Create(r.Context(), participants.AdminActor(g.ID), g.ID, in)
		if err != nil {
			v, berr := p.buildList(r.Context(), g, url.Values{})
			if berr != nil {
				p.fail(w, r, berr)
				return
			}
			v.Form = formOf(in)
			v.Form.Action, v.Form.SubmitKey = v.Base, "admin.people.add.submit"
			v.ErrorKey, v.ErrorField = errorKey(err, "admin.people.error.name_taken"), fieldOf(err, in)
			p.render(w, r, statusOf(err), v)
			return
		}
		p.journal(r.Context(), g, ActionPersonAdded, pt.ID)
		http.Redirect(w, r, p.base(g)+"?ok=added", http.StatusSeeOther)
	}))
}

// EditForm rend le formulaire de modification (GET .../{id}/modifier).
func (p *Pages) EditForm() http.Handler {
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
		v := p.baseView(g, "edit")
		v.BackURL = v.Base
		v.Form = formValues{FirstName: pt.FirstName, Name: pt.Name, Email: pt.Email, GiftHint: pt.GiftHint,
			Action: v.Base + "/" + strconv.FormatInt(id, 10) + "/modifier", SubmitKey: "admin.people.edit.submit", ID: id}
		p.render(w, r, http.StatusOK, v)
	})
}

// Edit enregistre la modification (POST .../{id}/modifier).
func (p *Pages) Edit() http.Handler {
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
		if _, err := p.person(r.Context(), g, id); err != nil {
			http.NotFound(w, r)
			return
		}
		in := inputFrom(r)
		if _, err := p.o.People.Update(r.Context(), participants.AdminActor(g.ID), g.ID, id, in); err != nil {
			v := p.baseView(g, "edit")
			v.BackURL = v.Base
			v.Form = formOf(in)
			v.Form.Action, v.Form.SubmitKey, v.Form.ID = v.Base+"/"+strconv.FormatInt(id, 10)+"/modifier", "admin.people.edit.submit", id
			v.ErrorKey, v.ErrorField = errorKey(err, "admin.people.error.name_taken"), fieldOf(err, in)
			p.render(w, r, statusOf(err), v)
			return
		}
		p.journal(r.Context(), g, ActionPersonEdit, id)
		http.Redirect(w, r, p.base(g)+"?ok=edited", http.StatusSeeOther)
	}))
}

// Remove supprime une personne après la confirmation intégrée à la page
// (POST .../{id}/supprimer, champ confirm=1).
func (p *Pages) Remove() http.Handler {
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
		if _, err := p.person(r.Context(), g, id); err != nil {
			http.NotFound(w, r)
			return
		}
		err := p.o.People.Delete(r.Context(), participants.AdminActor(g.ID), g.ID, id, r.PostForm.Get("confirm") == "1")
		if err == nil {
			if ferr := p.o.Rules.ForgetParticipant(r.Context(), g.ID, id); ferr != nil {
				err = ferr
			}
		}
		if err != nil {
			v, berr := p.buildList(r.Context(), g, url.Values{"supprimer": {strconv.FormatInt(id, 10)}})
			if berr != nil {
				p.fail(w, r, berr)
				return
			}
			v.DeleteError = true
			v.ErrorKey = errorKey(err, "admin.people.error.delete_after_draw")
			p.render(w, r, statusOf(err), v)
			return
		}
		p.journal(r.Context(), g, ActionPersonDel, 0)
		http.Redirect(w, r, p.base(g)+"?ok=deleted", http.StatusSeeOther)
	}))
}
