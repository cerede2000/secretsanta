package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// Six versions factices 0.1.0 a 0.1.5 ; la derniere porte aussi latest ;
// deux versions orphelines (sans tag) et une ancienne portant seulement un tag inconnu.
func fixture() Input {
	return Input{
		PackageVersions: []PackageVersion{
			{ID: 1, Tags: []string{"0.1.0"}},
			{ID: 2, Tags: []string{"0.1.1"}},
			{ID: 3, Tags: []string{"0.1.2"}},
			{ID: 4, Tags: []string{"0.1.3"}},
			{ID: 5, Tags: []string{"0.1.4"}},
			{ID: 6, Tags: []string{"0.1.5", "latest"}},
			{ID: 7, Tags: nil},
			{ID: 8, Tags: []string{}},
			{ID: 9, Tags: []string{"sha-abc"}},
		},
		GitTags: []string{"v0.1.0", "v0.1.1", "v0.1.2", "v0.1.3", "v0.1.4", "v0.1.5", "autre"},
	}
}

func TestComputeKeepsFour(t *testing.T) {
	p := Compute(fixture(), Keep)
	wantIDs := []int64{1, 2, 9} // 7 et 8 sont sans tag : jamais supprimes
	if !reflect.DeepEqual(p.DeletePackageVersionIDs, wantIDs) {
		t.Errorf("ids supprimes : got %v want %v", p.DeletePackageVersionIDs, wantIDs)
	}
	wantTags := []string{"v0.1.0", "v0.1.1"}
	if !reflect.DeepEqual(p.DeleteGitTags, wantTags) {
		t.Errorf("tags supprimes : got %v want %v", p.DeleteGitTags, wantTags)
	}
	deleted := map[int64]bool{}
	for _, id := range p.DeletePackageVersionIDs {
		deleted[id] = true
	}
	kept := 0
	for _, pv := range fixture().PackageVersions {
		if !deleted[pv.ID] && len(pv.Tags) > 0 && pv.Tags[0] != "sha-abc" {
			kept++
		}
	}
	if kept != 4 {
		t.Errorf("versions gardees : %d, attendu 4", kept)
	}
	if deleted[6] {
		t.Error("latest ne doit jamais etre supprime")
	}
}

func TestLatestOnOldVersionIsKept(t *testing.T) {
	in := fixture()
	in.PackageVersions[0].Tags = []string{"0.1.0", "latest"}
	p := Compute(in, Keep)
	for _, id := range p.DeletePackageVersionIDs {
		if id == 1 {
			t.Fatal("la version portant latest a ete supprimee")
		}
	}
}

func TestFewVersionsDeleteNothing(t *testing.T) {
	in := Input{
		PackageVersions: []PackageVersion{{ID: 1, Tags: []string{"0.1.0", "latest"}}},
		GitTags:         []string{"v0.1.0"},
	}
	p := Compute(in, Keep)
	if len(p.DeletePackageVersionIDs) != 0 || len(p.DeleteGitTags) != 0 {
		t.Fatalf("rien a supprimer, got %+v", p)
	}
}

func TestSemverOrderNotLexical(t *testing.T) {
	in := Input{GitTags: []string{"v0.1.2", "v0.1.9", "v0.1.10", "v0.1.11", "v0.1.12", "v0.1.3"}}
	p := Compute(in, Keep)
	want := []string{"v0.1.2", "v0.1.3"}
	if !reflect.DeepEqual(p.DeleteGitTags, want) {
		t.Fatalf("got %v want %v", p.DeleteGitTags, want)
	}
}

func TestRunJSON(t *testing.T) {
	in := `{"package_versions":[{"id":7,"tags":["sha-x"]}],"git_tags":[]}`
	var out bytes.Buffer
	if err := run(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	want := `{"delete_package_version_ids":[7],"delete_git_tags":[]}` + "\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
	if err := run(strings.NewReader("{"), &out); err == nil {
		t.Fatal("JSON invalide doit echouer")
	}
}
