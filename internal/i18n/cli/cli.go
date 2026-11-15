// Package cli implémente les sous-commandes « secretsanta i18n export » et
// « secretsanta i18n check ».
//
// Branchement attendu dans cmd/secretsanta : charger le catalogue avec
// catalog.Load(web.I18n, catalog.Options{DefaultLang: cfg.DefaultLang,
// ExternalDir: cfg.I18nDir}) puis os.Exit(cli.Run(os.Args[2:], os.Stdout,
// os.Stderr, cat)) lorsque os.Args[1] == "i18n".
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"secretsanta/internal/i18n/catalog"
)

const usage = `usage :
  secretsanta i18n export [-o fichier] <langue>   écrit un fichier plat à traduire
  secretsanta i18n check <fichier.json|langue>    contrôle une traduction
`

// Run exécute la sous-commande à partir des arguments qui suivent « i18n »
// (par exemple ["export", "de"]) et renvoie le code de sortie du processus :
// 0 si tout va bien, 1 au moindre écart ou erreur, 2 pour un usage incorrect.
func Run(args []string, stdout, stderr io.Writer, cat *catalog.Catalog) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "export":
		return runExport(args[1:], stdout, stderr, cat)
	case "check":
		return runCheck(args[1:], stdout, stderr, cat)
	default:
		fmt.Fprintf(stderr, "sous-commande inconnue : %q\n%s", args[0], usage)
		return 2
	}
}

func runExport(args []string, stdout, stderr io.Writer, cat *catalog.Catalog) int {
	fs := flag.NewFlagSet("i18n export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "fichier de sortie (défaut : sortie standard)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	lang := fs.Arg(0)
	var w io.Writer = stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(stderr, "export : %v\n", err)
			return 1
		}
		defer f.Close()
		w = f
	}
	if err := Export(w, cat, lang); err != nil {
		fmt.Fprintf(stderr, "export : %v\n", err)
		return 1
	}
	return 0
}

func runCheck(args []string, stdout, stderr io.Writer, cat *catalog.Catalog) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	target := args[0]
	var issues []Issue
	var err error
	if strings.HasSuffix(target, ".json") || strings.ContainsRune(target, os.PathSeparator) {
		issues, err = CheckFile(cat, target)
	} else {
		issues, err = CheckLang(cat, target)
	}
	if err != nil {
		fmt.Fprintf(stderr, "check : %v\n", err)
		return 1
	}
	for _, is := range issues {
		fmt.Fprintln(stdout, is.String())
	}
	if len(issues) > 0 {
		fmt.Fprintf(stderr, "%d écart(s) détecté(s)\n", len(issues))
		return 1
	}
	fmt.Fprintln(stdout, "ok : aucun écart")
	return 0
}
