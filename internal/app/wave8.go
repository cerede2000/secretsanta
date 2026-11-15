package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"secretsanta/internal/admin/audit"
	"secretsanta/internal/admin/dashboard"
	"secretsanta/internal/admin/menu"
	"secretsanta/internal/admin/roles"
	"secretsanta/internal/letterread"
	regadmin "secretsanta/internal/registrationpages/admin"
	"secretsanta/internal/shared/adminnav"
	"secretsanta/internal/targetpage"
)

// registerLetterRead construit la lecture de la lettre par le tireur et monte
// sa seule route, GET /{slug}/lettre-recue/{cible}/image/{idee}. Les cibles
// viennent du service du tirage construit avec la porte de révélation
// (reveal.NewGate) : rien n'est lu avant l'ouverture, ni une cible non
// découverte. Les pages de lecture viennent de F5c ; aucune route d'écriture
// n'existe pour le tireur. Elle suit registerLetter et registerDraw.
func registerLetterRead(a *App) error {
	a.LetterRead = letterread.NewService(a.DB, a.Clock, a.Draw, a.Letter)
	for _, r := range letterread.NewHandler(a.LetterRead, a.Groups).Routes() {
		a.Slugs.Handle(r.Method+" "+r.Pattern, r.Handler)
	}
	return nil
}

// registerTargetPage construit les pages du participant (attente, carte de
// cible, réception seule) et monte, derrière la chaîne CSRF puis sessions,
// GET /{slug}/participant et GET /{slug}/ma-cible (où la connexion et le
// courriel « tirage prêt » atterrissent), POST /{slug}/preference et POST
// /{slug}/budget-vu. Les gabarits sont ceux de web/templates/draw : chacun a
// son propre nom, aucun ne définit layout/content. Les chemins voisins
// (lettre, boîte, suggestions) gardent les valeurs par défaut du paquet : les
// pages de F5c et F8c ne sont pas encore montées.
func registerTargetPage(a *App) error {
	set, err := newTemplateSet(a.Cat, "draw")
	if err != nil {
		return err
	}
	p, err := targetpage.New(targetpage.Deps{
		Groups: a.Groups, Participants: a.Participants, Draw: a.Draw,
		Budget: targetpage.NewStore(a.DB), Render: set.render, Clock: a.Clock,
		DefaultLang: a.Cfg.DefaultLang, Location: a.Cfg.Location,
	})
	if err != nil {
		return fmt.Errorf("pages de la cible : %w", err)
	}
	a.TargetPage = p
	a.Slugs.Handle("GET /{slug}/participant", p.Home())
	a.Slugs.Handle("GET /{slug}/ma-cible", p.Home())
	a.Slugs.Handle("POST /{slug}/preference", p.Preference())
	a.Slugs.Handle("POST /{slug}/budget-vu", p.BudgetSeen())
	return nil
}

// navAliases traduit les clés actives posées par les pages en clés déclarées
// dans le registre : le tableau de bord passe « dashboard », le registre
// déclare « tableau ».
var navAliases = map[string]string{"dashboard": "tableau"}

// adminNav renvoie les entrées de navigation d'administration d'une requête :
// la clé active est ramenée à celle du registre et {slug} est remplacé par
// l'adresse du groupe du chemin (menu.ResolveNav).
func (a *App) adminNav(r *http.Request, active string) []adminnav.Item {
	if k, ok := navAliases[active]; ok {
		active = k
	}
	return menu.ResolveNav(a.AdminNav.Items(active), r.PathValue("slug"))
}

// auditAction convertit l'action d'une page (qui peut contenir un point, par
// exemple « roles.changement ») en code du journal (minuscules, chiffres et
// soulignés seulement).
func auditAction(action string) string { return strings.ReplaceAll(action, ".", "_") }

// registerAdminPages construit l'administration d'un groupe et monte ses pages
// sur Slugs, derrière la chaîne SecurityHeaders, CSRF, sessions : la feuille
// « Plus » et la déconnexion, le tableau de bord, le contrôle du tirage, le
// journal, la marque « lien remis » et le panneau des rôles. Chaque page exige
// la session administrateur de SON groupe (redirection vers la connexion
// sinon, jamais d'accès croisé) et analyse ses propres gabarits : chacune
// définit son bloc layout/content, un jeu cloné par page. Le registre de
// navigation est celui de F1b : Tableau, Participants, Tirage, Inscriptions et
// Plus sont tous déclarés ici (cinq entrées dans la barre du bas).
func registerAdminPages(a *App) error {
	a.AdminNav = adminnav.New()
	menu.DeclareNav(a.AdminNav)
	menu.DeclareParticipants(a.AdminNav)
	regadmin.DeclareNav(a.AdminNav)
	a.Audit = audit.New(a.DB, a.Clock)

	menuSet, err := newTemplateSet(a.Cat, "admin/menu")
	if err != nil {
		return err
	}
	mp, err := menu.New(menu.Deps{
		Groups: a.Groups, Sessions: a.Sessions, Render: a.plusMenuRender(menuSet.render), Nav: a.AdminNav,
		DefaultLang: a.Cfg.DefaultLang,
	})
	if err != nil {
		return fmt.Errorf("menu d'administration : %w", err)
	}
	mp.Register(a.Slugs)

	ds, err := dashboard.New(dashboard.Options{
		DB: a.DB, Clock: a.Clock, Participants: a.Participants, Draws: a.Draw, Constraints: a.Rules,
		Audit: a.Audit, Reveal: a.Reveal, Registrations: a.Verify,
	})
	if err != nil {
		return fmt.Errorf("tableau de bord : %w", err)
	}
	dp, err := dashboard.NewPages(dashboard.PageOptions{
		Service: ds, Groups: a.Groups, Translator: a.Cat, NavFor: a.adminNav,
	})
	if err != nil {
		return fmt.Errorf("pages du tableau de bord : %w", err)
	}
	dp.Register(a.Slugs)

	rp, err := roles.New(roles.Options{
		Groups: a.Groups, People: a.Participants, Roles: a.RuleChange, Check: a.Draw, State: a.DrawState,
		Translator: a.Cat, Nav: a.adminNav,
		Journal: func(ctx context.Context, groupID int64, action string, _ map[string]int) {
			// Le journal ne garde qu'un code : les comptes restent à l'écran.
			_ = a.Audit.RecordTx(ctx, a.DB, groupID, auditAction(action), 0)
		},
	})
	if err != nil {
		return fmt.Errorf("panneau des rôles : %w", err)
	}
	rp.Register(a.Slugs)
	a.Dashboard = ds
	return nil
}
