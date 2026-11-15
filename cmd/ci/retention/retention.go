// Commande retention : calcule ce qu'il faut supprimer pour ne garder que les
// 4 dernieres versions. Lit un JSON sur l'entree standard :
//
//	{"package_versions":[{"id":1,"tags":["0.1.0"]}],"git_tags":["v0.1.0"]}
//
// et ecrit le plan JSON : {"delete_package_version_ids":[...],"delete_git_tags":[...]}.
// Les releases portent le nom des tags Git : delete_git_tags vaut aussi pour elles.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Keep est le nombre de versions conservees.
const Keep = 4

// PackageVersion est une version de paquet GHCR avec ses tags d'image.
type PackageVersion struct {
	ID   int64    `json:"id"`
	Tags []string `json:"tags"`
}

// Input est l'etat courant du depot.
type Input struct {
	PackageVersions []PackageVersion `json:"package_versions"`
	GitTags         []string         `json:"git_tags"`
}

// Plan liste les suppressions a faire.
type Plan struct {
	DeletePackageVersionIDs []int64  `json:"delete_package_version_ids"`
	DeleteGitTags           []string `json:"delete_git_tags"`
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

type semver [3]int

func parse(s string) (semver, bool) {
	m := semverRe.FindStringSubmatch(s)
	if m == nil {
		return semver{}, false
	}
	var v semver
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return semver{}, false
		}
		v[i] = n
	}
	return v, true
}

func less(a, b semver) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// newest renvoie l'ensemble des keep plus recentes versions parmi names.
func newest(names []string, keep int) map[semver]bool {
	set := map[semver]bool{}
	for _, n := range names {
		if v, ok := parse(n); ok {
			set[v] = true
		}
	}
	all := make([]semver, 0, len(set))
	for v := range set {
		all = append(all, v)
	}
	sort.Slice(all, func(i, j int) bool { return less(all[j], all[i]) })
	if len(all) > keep {
		all = all[:keep]
	}
	kept := map[semver]bool{}
	for _, v := range all {
		kept[v] = true
	}
	return kept
}

// Compute calcule le plan de retention. Une version de paquet n'est jamais
// supprimee si elle porte latest ou un tag parmi les keep plus recents ;
// les versions sans tag ou sans tag de version sont supprimees. Les tags Git
// v<version> hors des keep plus recents sont supprimes (et leurs releases).
func Compute(in Input, keep int) Plan {
	var imageTags []string
	for _, pv := range in.PackageVersions {
		imageTags = append(imageTags, pv.Tags...)
	}
	keptImg := newest(imageTags, keep)

	plan := Plan{DeletePackageVersionIDs: []int64{}, DeleteGitTags: []string{}}
	for _, pv := range in.PackageVersions {
		protect := false
		for _, t := range pv.Tags {
			if t == "latest" {
				protect = true
			}
			if v, ok := parse(t); ok && keptImg[v] {
				protect = true
			}
		}
		if !protect {
			plan.DeletePackageVersionIDs = append(plan.DeletePackageVersionIDs, pv.ID)
		}
	}

	keptGit := newest(in.GitTags, keep)
	for _, t := range in.GitTags {
		t = strings.TrimSpace(t)
		v, ok := parse(t)
		if !ok || !strings.HasPrefix(t, "v") {
			continue
		}
		if !keptGit[v] {
			plan.DeleteGitTags = append(plan.DeleteGitTags, t)
		}
	}
	sort.Slice(plan.DeletePackageVersionIDs, func(i, j int) bool {
		return plan.DeletePackageVersionIDs[i] < plan.DeletePackageVersionIDs[j]
	})
	sort.Strings(plan.DeleteGitTags)
	return plan
}

func run(in io.Reader, out io.Writer) error {
	var input Input
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return fmt.Errorf("entree invalide : %w", err)
	}
	return json.NewEncoder(out).Encode(Compute(input, Keep))
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
