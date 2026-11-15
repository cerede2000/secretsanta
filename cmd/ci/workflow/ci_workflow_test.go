package workflow

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func load(t *testing.T) (string, []string) {
	t.Helper()
	b, err := os.ReadFile("../../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	return s, strings.Split(s, "\n")
}

// block renvoie les lignes indentees sous la cle de premier niveau ou de job donnee.
func block(lines []string, header string, indent int) []string {
	var out []string
	in := false
	pad := strings.Repeat(" ", indent)
	for _, l := range lines {
		if !in {
			if l == pad+header {
				in = true
			}
			continue
		}
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, pad+" ") {
			break
		}
		out = append(out, l)
	}
	return out
}

func TestSyntaxeDeBase(t *testing.T) {
	s, lines := load(t)
	if strings.Contains(s, "\t") {
		t.Error("tabulation interdite en YAML")
	}
	for _, k := range []string{"name:", "on:", "permissions:", "jobs:"} {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, k) {
				found = true
			}
		}
		if !found {
			t.Errorf("cle de premier niveau %q absente", k)
		}
	}
	if len(block(lines, "checks:", 2)) == 0 || len(block(lines, "publish:", 2)) == 0 {
		t.Fatal("jobs checks et publish attendus")
	}
}

func TestDeclencheursEtPermissions(t *testing.T) {
	_, lines := load(t)
	on := strings.Join(block(lines, "on:", 0), "\n")
	if !strings.Contains(on, "pull_request") {
		t.Error("pull_request doit declencher les controles")
	}
	if !regexp.MustCompile(`(?m)^  push:\n    branches: \[main\]`).MatchString(on) {
		t.Error("push limite a la branche main attendu")
	}
	perms := strings.Join(block(lines, "permissions:", 0), "\n")
	if !strings.Contains(perms, "contents: write") || !strings.Contains(perms, "packages: write") {
		t.Error("permissions contents et packages en ecriture attendues")
	}
	if strings.Contains(perms, "id-token") || strings.Contains(perms, "actions:") {
		t.Error("permissions minimales : rien d'autre")
	}
}

func TestControlesAvantPublication(t *testing.T) {
	_, lines := load(t)
	checks := strings.Join(block(lines, "checks:", 2), "\n")
	for _, want := range []string{
		`gofmt -l .`, "go vet ./...", "go test ./...",
		"CGO_ENABLED=0 go build", "govulncheck ./...",
	} {
		if !strings.Contains(checks, want) {
			t.Errorf("controle %q absent du job checks", want)
		}
	}
	if strings.Contains(checks, "push: true") || strings.Contains(checks, "build-push-action") {
		t.Error("le job checks ne doit rien publier")
	}
}

func TestPublicationSeulementDepuisMain(t *testing.T) {
	s, lines := load(t)
	pub := strings.Join(block(lines, "publish:", 2), "\n")
	if !regexp.MustCompile(`(?m)^    needs: checks$`).MatchString(pub) {
		t.Error("publish doit dependre de checks")
	}
	if !strings.Contains(pub, "if: github.event_name == 'push' && github.ref == 'refs/heads/main'") {
		t.Error("publish doit etre conditionne a un push sur main")
	}
	if strings.Contains(pub, "event_name == 'pull_request'") {
		t.Error("aucune publication par pull_request")
	}
	// Toute publication (push d'image, tag, suppression) vit dans le job publish.
	idx := strings.Index(s, "  publish:")
	before := s[:idx]
	for _, forbidden := range []string{"push: true", "docker/login-action", "git push", "gh release", "gh api -X DELETE"} {
		if strings.Contains(before, forbidden) {
			t.Errorf("%q hors du job publish", forbidden)
		}
	}
}

func TestImageAmd64EtVersions(t *testing.T) {
	s, lines := load(t)
	pub := strings.Join(block(lines, "publish:", 2), "\n")
	if !strings.Contains(pub, "platforms: linux/amd64") {
		t.Error("linux/amd64 attendu")
	}
	if !strings.Contains(pub, "ghcr.io/cerede2000/secretsanta") {
		t.Error("image ghcr.io/cerede2000/secretsanta attendue")
	}
	if !strings.Contains(pub, ":latest") || !strings.Contains(pub, "steps.version.outputs.version") {
		t.Error("tags <version> et latest attendus")
	}
	low := strings.ToLower(s)
	for _, bad := range []string{"arm64", "github.sha"} {
		if strings.Contains(low, bad) {
			t.Errorf("%q interdit dans le workflow", bad)
		}
	}
	for _, tool := range []string{"./cmd/ci/version", "./cmd/ci/retention", `git tag "v`} {
		if !strings.Contains(pub, tool) {
			t.Errorf("%q attendu dans publish", tool)
		}
	}
}
