package config

import (
	"path/filepath"
	"testing"
)

func dkim(t *testing.T, kv ...string) map[string]string {
	t.Helper()
	m := with(VarMailDKIMDomain, "example.org", VarMailDKIMSelector, "noel", VarMailDKIMKeyFile, writeKey(t, fakePEM), VarMailDKIMAlgorithm, "rsa-sha256")
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == "" {
			delete(m, kv[i])
		} else {
			m[kv[i]] = kv[i+1]
		}
	}
	return m
}

func TestDKIMComplete(t *testing.T) {
	for _, alg := range []string{"rsa-sha256", "ed25519-sha256"} {
		c, err := Load(env(dkim(t, VarMailDKIMAlgorithm, alg)))
		if err != nil || !c.DKIM.Enabled() || c.DKIM.Algorithm != alg || len(c.DKIM.Headers) == 0 {
			t.Fatalf("%s : %v", alg, err)
		}
	}
	c, err := Load(env(dkim(t, VarMailDKIMHeaders, "From, To,Subject")))
	if err != nil || len(c.DKIM.Headers) != 3 {
		t.Fatalf("en-têtes : %v", err)
	}
}

func TestDKIMAbsentIsFine(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil || c.DKIM.Enabled() {
		t.Fatal("DKIM doit être facultatif")
	}
}

func TestDKIMIncompleteRefused(t *testing.T) {
	for _, v := range []string{VarMailDKIMDomain, VarMailDKIMSelector, VarMailDKIMKeyFile, VarMailDKIMAlgorithm} {
		m := dkim(t, v, "")
		msg := mustFail(t, m, v)
		if msg == "" {
			t.Fatal("message vide")
		}
	}
}

func TestDKIMUnknownAlgorithm(t *testing.T) {
	mustFail(t, dkim(t, VarMailDKIMAlgorithm, "rsa-sha1"), VarMailDKIMAlgorithm)
}

func TestDKIMUnreadableFile(t *testing.T) {
	mustFail(t, dkim(t, VarMailDKIMKeyFile, filepath.Join(t.TempDir(), "absent.pem")), VarMailDKIMKeyFile)
	mustFail(t, dkim(t, VarMailDKIMKeyFile, writeKey(t, "pas une cle")), VarMailDKIMKeyFile)
	mustFail(t, dkim(t, VarMailDKIMKeyFile, t.TempDir()), VarMailDKIMKeyFile)
}

func TestDKIMPrivateKeyInlineRefused(t *testing.T) {
	mustFail(t, dkim(t, VarMailDKIMKeyFile, fakePEM), VarMailDKIMKeyFile)
	mustFail(t, dkim(t, "MAIL_DKIM_PRIVATE_KEY", fakePEM), "MAIL_DKIM_PRIVATE_KEY")
}

func TestDKIMBadHeadersAndDomain(t *testing.T) {
	mustFail(t, dkim(t, VarMailDKIMHeaders, "To,Subject"), VarMailDKIMHeaders)
	mustFail(t, dkim(t, VarMailDKIMHeaders, "From,Bad Header"), VarMailDKIMHeaders)
	mustFail(t, dkim(t, VarMailDKIMDomain, "pas un domaine"), VarMailDKIMDomain)
	mustFail(t, dkim(t, VarMailDKIMSelector, "bad selector"), VarMailDKIMSelector)
}
