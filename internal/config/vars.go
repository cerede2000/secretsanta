package config

// Nom des variables d'environnement. Source unique : toute variable lue par
// l'application doit figurer dans Variables et dans .env.example.
const (
	VarPublicURL        = "PUBLIC_URL"
	VarListenAddr       = "LISTEN_ADDR"
	VarDataDir          = "DATA_DIR"
	VarInstancePassword = "INSTANCE_PASSWORD"
	VarSigningSecret    = "SIGNING_SECRET"
	VarTimezone         = "APP_TIMEZONE"

	VarSMTPHost     = "SMTP_HOST"
	VarSMTPPort     = "SMTP_PORT"
	VarSMTPUser     = "SMTP_USER"
	VarSMTPPassword = "SMTP_PASSWORD"
	VarMailFrom     = "MAIL_FROM"
	VarMailFromName = "MAIL_FROM_NAME"

	VarMailEHLOHostname  = "MAIL_EHLO_HOSTNAME"
	VarMailEnvelopeFrom  = "MAIL_ENVELOPE_FROM"
	VarMailReplyTo       = "MAIL_REPLY_TO"
	VarMailTLSMode       = "MAIL_TLS_MODE"
	VarMailAllowInsecure = "MAIL_ALLOW_INSECURE"
	VarMailMaxPerMinute  = "MAIL_MAX_PER_MINUTE"

	VarMailDKIMDomain    = "MAIL_DKIM_DOMAIN"
	VarMailDKIMSelector  = "MAIL_DKIM_SELECTOR"
	VarMailDKIMKeyFile   = "MAIL_DKIM_KEY_FILE"
	VarMailDKIMAlgorithm = "MAIL_DKIM_ALGORITHM"
	VarMailDKIMHeaders   = "MAIL_DKIM_HEADERS"

	VarRemindersEnabled = "REMINDERS_ENABLED"

	VarSearxngURL   = "SEARXNG_URL"
	VarBraveAPIKey  = "BRAVE_API_KEY"
	VarSerperAPIKey = "SERPER_API_KEY"
)

// Variables liste toutes les variables déclarées.
var Variables = []string{
	VarPublicURL, VarListenAddr, VarDataDir, VarInstancePassword, VarSigningSecret, VarTimezone,
	VarSMTPHost, VarSMTPPort, VarSMTPUser, VarSMTPPassword, VarMailFrom, VarMailFromName,
	VarMailEHLOHostname, VarMailEnvelopeFrom, VarMailReplyTo, VarMailTLSMode, VarMailAllowInsecure, VarMailMaxPerMinute,
	VarMailDKIMDomain, VarMailDKIMSelector, VarMailDKIMKeyFile, VarMailDKIMAlgorithm, VarMailDKIMHeaders,
	VarRemindersEnabled,
	VarSearxngURL, VarBraveAPIKey, VarSerperAPIKey,
}

// RequiredVariables n'ont aucun défaut : le démarrage échoue sans elles.
var RequiredVariables = []string{VarInstancePassword, VarSigningSecret}

// varPrivateKeyInEnv est un nom de variable interdit : une clé privée ne
// passe jamais en clair dans l'environnement.
const varPrivateKeyInEnv = "MAIL_DKIM_PRIVATE_KEY"
