package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{VarInstancePassword: "mdp-instance-xyz", VarSigningSecret: "signature-secrete-abc"}
}

func with(kv ...string) map[string]string {
	m := base()
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func mustFail(t *testing.T, m map[string]string, wantVar string) string {
	t.Helper()
	_, err := Load(env(m))
	if err == nil {
		t.Fatalf("attendu un refus nommant %s", wantVar)
	}
	if !strings.Contains(err.Error(), wantVar) {
		t.Fatalf("le message ne nomme pas %s : %v", wantVar, err)
	}
	return err.Error()
}

func writeKey(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dkim.pem")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const fakePEM = "-----BEGIN PRIVATE KEY-----\nTOPSECRETKEYMATERIAL\n-----END PRIVATE KEY-----\n"
