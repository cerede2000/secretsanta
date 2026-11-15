package maildiagpages_test

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"secretsanta/internal/mail/queue"
	"secretsanta/internal/maildiag"
)

var paramRe = regexp.MustCompile(`\{([A-Za-z_]+)\}`)

func paramsOf(s string) string {
	var ps []string
	for _, m := range paramRe.FindAllStringSubmatch(s, -1) {
		ps = append(ps, m[1])
	}
	sort.Strings(ps)
	return strings.Join(ps, ",")
}

func fragments(v string) []string {
	var out []string
	for _, part := range paramRe.Split(v, -1) {
		if part = strings.TrimSpace(part); len([]rune(part)) >= 8 {
			out = append(out, part)
		}
	}
	return out
}

var rawKeyRe = regexp.MustCompile(`\bmaildiag\.(?:title|back|report|checks|status|check|code|copy|test|mail|unverifiable|suppression)\b[a-z_.]*`)

func TestCatalogsParity(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	for k, v := range fr {
		if !strings.HasPrefix(k, "maildiag.") {
			t.Errorf("clé %q sans préfixe du module", k)
		}
		e, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente en anglais", k)
			continue
		}
		if paramsOf(v) != paramsOf(e) {
			t.Errorf("clé %q : paramètres différents", k)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente en français", k)
		}
	}
}

// Chaque code du diagnostic a son texte dans les catalogues.
func TestEveryDiagnosticCodeHasText(t *testing.T) {
	fr := catalogFile(t, "fr")
	codes := []string{"mail.disabled",
		"spf.lookup_failed", "spf.missing", "spf.multiple", "spf.hardfail", "spf.softfail", "spf.pass_all", "spf.neutral_all", "spf.redirect", "spf.no_all",
		"dkim.off", "dkim.key_unreadable", "dkim.lookup_failed", "dkim.ok", "dkim.missing", "dkim.mismatch",
		"dmarc.lookup_failed", "dmarc.missing", "dmarc.multiple", "dmarc.none", "dmarc.quarantine", "dmarc.reject", "dmarc.invalid",
		"mx.lookup_failed", "mx.missing", "mx.ok",
		"alignment_envelope.ok", "alignment_envelope.misaligned", "alignment_dkim.off", "alignment_dkim.ok", "alignment_dkim.misaligned",
		"smtp_tls.ok", "smtp_tls.unreachable", "smtp_tls.cert_expired", "smtp_tls.hostname", "smtp_tls.untrusted", "smtp_tls.handshake_failed", "smtp_tls.none_configured", "smtp_tls.no_encryption",
		"public_https.ok", "public_https.invalid", "public_https.local", "public_https.not_https", "public_https.cert_expired", "public_https.hostname", "public_https.untrusted", "public_https.unreachable",
		"public_health.ok", "public_health.unreachable", "public_health.bad_status"}
	for _, c := range codes {
		if _, ok := fr["maildiag.code."+c]; !ok {
			t.Errorf("code %q sans texte", c)
		}
	}
	for _, id := range []string{"mail", maildiag.CheckSPF, maildiag.CheckDKIM, maildiag.CheckDMARC, maildiag.CheckMX, maildiag.CheckAlignEnv,
		maildiag.CheckAlignDKIM, maildiag.CheckSMTPTLS, maildiag.CheckPublicHTTPS, maildiag.CheckPublicHealth} {
		if _, ok := fr["maildiag.check."+id]; !ok {
			t.Errorf("point %q sans titre", id)
		}
	}
	for _, nv := range maildiag.NotVerifiableItems() {
		if _, ok := fr["maildiag.unverifiable."+nv.ID]; !ok {
			t.Errorf("non vérifiable %q sans texte", nv.ID)
		}
	}
}

// F6c.AC15 : le rendu anglais n'a ni clé brute ni texte français.
func TestEnglishRenderHasNoRawKeyNorFrench(t *testing.T) {
	fr, en := catalogFile(t, "fr"), catalogFile(t, "en")
	e := newEnv(t)
	_ = e.supp.Add(t.Context(), e.gid, "rebond@exemple.fr", "550")
	e.mailer.st = queue.Status{State: queue.StateSent, SentAt: t0}
	pages := map[string]string{
		"page":  e.get(pagePath, "en").Body.String(),
		"test":  e.post(t, testPath, "en", url.Values{"to": {"camille@exemple.fr"}}).Body.String(),
		"error": e.post(t, testPath, "en", url.Values{"to": {"nope"}}).Body.String(),
	}
	for name, body := range pages {
		if !strings.Contains(body, `<html lang="en">`) {
			t.Errorf("%s : lang=en absent", name)
		}
		if m := rawKeyRe.FindString(body); m != "" {
			t.Errorf("%s : clé brute %q", name, m)
		}
		text := html.UnescapeString(body)
		for k, v := range fr {
			if v == en[k] {
				continue
			}
			for _, frag := range fragments(v) {
				if strings.Contains(text, frag) {
					t.Errorf("%s : texte français %q (%s)", name, frag, k)
				}
			}
		}
	}
	if !strings.Contains(pages["page"], "Run the diagnostic again") || !strings.Contains(pages["error"], "Enter a valid email address.") {
		t.Error("textes anglais attendus absents")
	}
}
