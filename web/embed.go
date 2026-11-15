// Package web embarque les ressources de l'interface : gabarits, feuilles de
// style et catalogues de textes. Fichier d'intégration, étendu par F1c.
package web

import "embed"

// I18n contient les fichiers de textes (static/i18n/<module>/<langue>.json,
// quelle que soit la profondeur des sous-dossiers d'un module).
//
//go:embed static/i18n
var I18n embed.FS

// Templates contient les gabarits html/template (templates/<module>/*.html).
//
//go:embed templates
var Templates embed.FS

// Static contient les ressources servies sous /static/ (CSS, scripts, images),
// catalogues de textes compris.
//
//go:embed static
var Static embed.FS
