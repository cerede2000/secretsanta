package config

import (
	"fmt"
	"log/slog"
	"strings"
)

func presence(set bool) string {
	if set {
		return "défini"
	}
	return "non défini"
}

// String donne une représentation lisible sans aucun secret (mots de passe,
// clés d'API, chemin de clé DKIM).
func (c *Config) String() string {
	var b strings.Builder
	for _, kv := range c.Summary() {
		fmt.Fprintf(&b, "%s=%s\n", kv[0], kv[1])
	}
	return b.String()
}

// Summary liste les réglages utiles au journal de démarrage ; les secrets
// n'apparaissent que sous la forme « défini » ou « non défini ».
func (c *Config) Summary() [][2]string {
	return [][2]string{
		{VarPublicURL, c.PublicURL},
		{VarListenAddr, c.ListenAddr},
		{VarDataDir, c.DataDir},
		{VarTimezone, c.Timezone},
		{VarInstancePassword, presence(c.InstancePass.IsSet())},
		{VarSigningSecret, presence(c.SigningKey.IsSet())},
		{VarSMTPHost, c.SMTP.Host},
		{VarSMTPUser, presence(c.SMTP.User != "")},
		{VarSMTPPassword, presence(c.SMTP.Password.IsSet())},
		{VarMailFrom, c.SMTP.From},
		{VarMailTLSMode, c.Mail.TLSMode},
		{VarMailMaxPerMinute, fmt.Sprint(c.Mail.MaxPerMinute)},
		{VarMailDKIMDomain, c.DKIM.Domain},
		{VarMailDKIMKeyFile, presence(c.DKIM.KeyFile.IsSet())},
		{VarMailDKIMAlgorithm, c.DKIM.Algorithm},
		{VarRemindersEnabled, fmt.Sprint(c.RemindersEnabled)},
		{VarSearxngURL, c.Sources.SearxngURL},
		{VarBraveAPIKey, presence(c.Sources.BraveKey.IsSet())},
		{VarSerperAPIKey, presence(c.Sources.SerperKey.IsSet())},
	}
}

// LogValue permet de passer la configuration à slog sans fuite.
func (c *Config) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, 20)
	for _, kv := range c.Summary() {
		attrs = append(attrs, slog.String(kv[0], kv[1]))
	}
	return slog.GroupValue(attrs...)
}
