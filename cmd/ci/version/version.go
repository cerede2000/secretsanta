// Commande version : calcule la prochaine version publiée.
// Usage : version <fichier VERSION> < liste de tags Git (un par ligne).
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var baseRe = regexp.MustCompile(`^\d+\.\d+$`)

// Next renvoie base.patch où patch = (plus grand patch existant de la ligne base) + 1,
// ou 0 sans tag. Les tags attendus ont la forme v<base>.<patch>.
// Le plus grand patch plutôt que le nombre de tags : la rétention supprime les anciens tags.
func Next(base string, tags []string) (string, error) {
	base = strings.TrimSpace(base)
	if !baseRe.MatchString(base) {
		return "", fmt.Errorf("VERSION invalide %q : attendu majeure.mineure", base)
	}
	prefix := "v" + base + "."
	next := 0
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(t, prefix))
		if err != nil || n < 0 {
			continue
		}
		if n+1 > next {
			next = n + 1
		}
	}
	return fmt.Sprintf("%s.%d", base, next), nil
}

func run(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage : version <fichier VERSION> < tags")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var tags []string
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		tags = append(tags, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}
	v, err := Next(string(raw), tags)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, v)
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
