package sources

import (
	"context"
	"testing"

	"secretsanta/internal/config"
)

func TestCatalogEveryBandAndLanguage(t *testing.T) {
	c := NewCatalog()
	for _, lang := range []string{"fr", "en"} {
		for _, b := range Bands {
			res, err := c.Search(context.Background(), Query{Band: b, Lang: lang})
			if err != nil || len(res) < 3 {
				t.Fatalf("%s/%s : %d résultats, err=%v", lang, b, len(res), err)
			}
			for _, s := range res {
				if s.Title == "" || s.Description == "" {
					t.Fatalf("entrée vide %s/%s", lang, b)
				}
			}
		}
	}
}

func TestBandFor(t *testing.T) {
	v := func(n int64) *int64 { return &n }
	cases := map[Band]*int64{BandAll: nil, BandUnder20: v(1500), Band20to50: v(2000), Band50to100: v(5000), BandOver100: v(10000)}
	for want, in := range cases {
		if got := BandFor(in); got != want {
			t.Errorf("BandFor=%s want %s", got, want)
		}
	}
}

func TestChainWorksWithNoSourceVariable(t *testing.T) {
	cfg, err := config.Load(func(k string) string { return baseEnv()[k] })
	if err != nil {
		t.Fatal(err)
	}
	c := FromConfig(cfg.Sources, nil, nil)
	if n := c.Names(); len(n) != 1 || n[0] != "catalogue" {
		t.Fatalf("%v", n)
	}
	res, err := c.Search(context.Background(), Query{})
	if err != nil || len(res) == 0 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func baseEnv() map[string]string {
	return map[string]string{config.VarInstancePassword: "mot-de-passe-instance", config.VarSigningSecret: "0123456789abcdef0123456789abcdef0123456789abcdef"}
}
