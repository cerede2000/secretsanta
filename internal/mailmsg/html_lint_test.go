package mailmsg

import (
	"testing"
)

// F6b.AC6 : le contrôle du HTML refuse une image distante, un pixel de
// suivi, un script, une feuille de style externe, une image sans alt, un lien
// non https, un lien hors de l'hôte de PUBLIC_URL (hors désinscription) et un
// raccourcisseur d'adresse.

const pngData = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

var lintPolicy = Policy{
	PublicHost:        "noel.example.org",
	UnsubscribeURL:    "https://desabonnement.example.com/u/jeton",
	UnsubscribeMailto: "mailto:desinscription@example.org",
}

func TestHTMLLintRefusals(t *testing.T) {
	cases := []struct {
		name string
		html string
		cat  error
	}{
		{"image distante https", `<p><img src="https://noel.example.org/logo.png" alt="Logo"></p>`, ErrHTML},
		{"image distante http", `<p><img src="http://cdn.example.com/a.png" alt="a"></p>`, ErrHTML},
		{"image distante sans schéma", `<p><img src="//cdn.example.com/a.png" alt="a"></p>`, ErrHTML},
		{"image relative", `<p><img src="/logo.png" alt="a"></p>`, ErrHTML},
		{"image svg embarquée", `<p><img src="data:image/svg+xml;base64,PHN2Zz4=" alt="a"></p>`, ErrHTML},
		{"image en srcset", `<p><img src="` + pngData + `" srcset="https://cdn.example.com/a.png 2x" alt="a"></p>`, ErrHTML},
		{"fond distant", `<table background="https://cdn.example.com/f.png"><tr><td>x</td></tr></table>`, ErrHTML},
		{"fond distant en style", `<div style="background:url(https://cdn.example.com/f.png)">x</div>`, ErrHTML},
		{"pixel 1x1", `<p><img src="` + pngData + `" alt="" width="1" height="1"></p>`, ErrHTML},
		{"pixel 0", `<p><img src="` + pngData + `" alt="" width="0"></p>`, ErrHTML},
		{"pixel en style", `<p><img src="` + pngData + `" alt="" style="width:1px;height:1px"></p>`, ErrHTML},
		{"pixel masqué", `<p><img src="` + pngData + `" alt="" style="display:none"></p>`, ErrHTML},
		{"script", `<p>x</p><script>alert(1)</script>`, ErrHTML},
		{"script externe", `<script src="https://noel.example.org/a.js"></script>`, ErrHTML},
		{"gestionnaire", `<p onclick="alert(1)">x</p>`, ErrHTML},
		{"gestionnaire image", `<img src="` + pngData + `" alt="a" onerror="alert(1)">`, ErrHTML},
		{"feuille externe", `<link rel="stylesheet" href="https://noel.example.org/a.css"><p>x</p>`, ErrHTML},
		{"import css", `<style>@import url("https://cdn.example.com/a.css");</style><p>x</p>`, ErrHTML},
		{"url css", `<style>p{background:url(https://cdn.example.com/a.png)}</style><p>x</p>`, ErrHTML},
		{"commentaire conditionnel", `<!--[if mso]><img src="https://t.example.com/p.gif"><![endif]--><p>x</p>`, ErrHTML},
		{"iframe", `<iframe src="https://noel.example.org/"></iframe>`, ErrHTML},
		{"formulaire", `<form><button>ok</button></form>`, ErrHTML},
		{"meta refresh", `<meta http-equiv="refresh" content="0;url=https://evil.example.com">`, ErrHTML},
		{"base", `<base href="https://evil.example.com/">`, ErrHTML},
		{"image sans alt", `<p><img src="` + pngData + `"></p>`, ErrHTML},
		{"document sans lang fr", `<!DOCTYPE html><html lang="en"><body><p>x</p></body></html>`, ErrHTML},
		{"document sans lang", `<html><body><p>x</p></body></html>`, ErrHTML},
		{"balise non fermée", `<p><strong>x</p>`, ErrHTML},
		{"fin inattendue", `<p>x</p></div>`, ErrHTML},
		{"lien http", `<p><a href="http://noel.example.org/x">x</a></p>`, ErrLink},
		{"lien javascript", `<p><a href="javascript:alert(1)">x</a></p>`, ErrLink},
		{"lien relatif", `<p><a href="/x">x</a></p>`, ErrLink},
		{"ancre", `<p><a href="#haut">x</a></p>`, ErrLink},
		{"lien hors hôte", `<p><a href="https://evil.example.com/x">x</a></p>`, ErrLink},
		{"lien sous-domaine", `<p><a href="https://evil.noel.example.org/x">x</a></p>`, ErrLink},
		{"lien autre port", `<p><a href="https://noel.example.org:8443/x">x</a></p>`, ErrLink},
		{"lien piégé par identifiant", `<p><a href="https://noel.example.org@evil.example.com/x">x</a></p>`, ErrLink},
		{"mailto non déclaré", `<p><a href="mailto:autre@example.org">x</a></p>`, ErrLink},
		{"raccourcisseur", `<p><a href="https://bit.ly/abc">x</a></p>`, ErrLink},
		{"raccourcisseur sous-domaine", `<p><a href="https://www.tinyurl.com/abc">x</a></p>`, ErrLink},
		{"raccourcisseur cité", `<p>Voir bit.ly/abc</p>`, ErrLink},
		{"lien http en texte visible", `<p>Voir http://noel.example.org/x</p>`, ErrLink},
		{"lien hors hôte en texte visible", `<p>Voir https://evil.example.com/x</p>`, ErrLink},
		{"lien prolongé par une balise en ligne", `<p>https://noel.example.org<b>.evil.com</b>/x</p>`, ErrLink},
		{"lien coupé par une balise", `<p><b>https://</b>evil.example.com/x</p>`, ErrLink},
		{"www sans schéma", `<p>Voir www.example.com</p>`, ErrLink},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertKind(t, LintHTML(c.html, lintPolicy), c.cat)
		})
	}
}

func TestHTMLLintAccepts(t *testing.T) {
	for name, src := range map[string]string{
		"fragment":            `<p>Bonjour <strong>Léa</strong>,</p><p><a href="https://noel.example.org/famille">Voir</a></p>`,
		"image embarquée":     `<p><img src="` + pngData + `" alt="Sapin" width="64" height="64"></p>`,
		"image décorative":    `<p><img src="` + pngData + `" alt=""></p>`,
		"style local":         `<style>p { color: #1f2d26; }</style><p class="a" style="color:#c41e3a">x</p>`,
		"désinscription":      `<p><a href="https://desabonnement.example.com/u/jeton">Ne plus recevoir</a></p>`,
		"mailto déclaré":      `<p><a href="mailto:desinscription@example.org">Écrire</a></p>`,
		"document complet":    `<!DOCTYPE html><html lang="fr"><head><meta charset="utf-8"><title>T</title></head><body><p>x</p></body></html>`,
		"document fr-FR":      `<!DOCTYPE html><html lang="fr-FR"><body><p>x</p></body></html>`,
		"ponctuation de lien": `<p>Ton lien : https://noel.example.org/x.</p>`,
		"saut de ligne":       `<p>a<br>b<br/>c</p><hr>`,
		"blocs séparés":       `<p>https://noel.example.org</p><p>example</p>`,
		"hôte en majuscules":  `<p><a href="https://NOEL.example.org/x">x</a></p>`,
	} {
		if err := LintHTML(src, lintPolicy); err != nil {
			t.Errorf("%s refusé : %v", name, err)
		}
	}
}

func TestTextLintLinks(t *testing.T) {
	for _, s := range []string{
		"Voir http://noel.example.org/x",
		"Voir https://evil.example.com/x",
		"Voir https://bit.ly/x",
		"Voir t.co/x",
		"Voir www.noel.example.org",
		"Écrire à mailto:autre@example.org",
		"adresse:https://evil.example.com/x",
	} {
		if err := LintText(s, lintPolicy); err == nil {
			t.Errorf("%q accepté", s)
		}
	}
	for _, s := range []string{
		"Ton lien : https://noel.example.org/x.",
		"(https://noel.example.org/x)",
		"Date: 24 décembre, Remarque:rien",
		"Désinscription : https://desabonnement.example.com/u/jeton",
		"Contact : organisation@example.org",
	} {
		if err := LintText(s, lintPolicy); err != nil {
			t.Errorf("%q refusé : %v", s, err)
		}
	}
}

func TestBuildAppliesHTMLAndLinkLint(t *testing.T) {
	b := newBuilder(t, nil)
	m := validMessage()
	m.HTML += `<p><img src="https://noel.example.org/p.gif" alt=""></p>`
	_, err := b.Build(m)
	assertKind(t, err, ErrHTML)

	m = validMessage()
	m.Text += "https://evil.example.com/x\n"
	m.HTML += `<p><a href="https://evil.example.com/x">https://evil.example.com/x</a></p>`
	_, err = b.Build(m)
	assertKind(t, err, ErrLink)

	// PUBLIC_URL en http : ses liens http restent refusés (https uniquement).
	b = newBuilder(t, func(o *Options) { o.PublicURL = "http://noel.example.org" })
	m = validMessage()
	m.Text = "Ton lien : http://noel.example.org/famille\n"
	m.HTML = `<p>Ton lien : <a href="http://noel.example.org/famille">http://noel.example.org/famille</a></p>`
	_, err = b.Build(m)
	assertKind(t, err, ErrLink)
}
