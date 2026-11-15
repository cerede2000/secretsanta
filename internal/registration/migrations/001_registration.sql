-- Inscription par lien invité (F11.6). Instants en nanosecondes Unix (UTC).
-- Aucun secret n'est stocké en clair : le jeton d'invitation, le code de
-- vérification et le jeton de confirmation n'existent ici que sous forme
-- d'empreinte HMAC-SHA-256 (clé SIGNING_SECRET).

-- Mode « inscription par lien » d'un groupe. Sans ligne, le mode est
-- désactivé. token_hash : condensé HMAC brut (32 octets) du jeton en vigueur,
-- NULL tant qu'aucun jeton n'est émis ou après désactivation.
CREATE TABLE registration_invitations (
	group_id   INTEGER NOT NULL PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
	enabled    INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
	token_hash BLOB CHECK (token_hash IS NULL OR (typeof(token_hash) = 'blob' AND length(token_hash) = 32)),
	capacity   INTEGER NOT NULL DEFAULT 0 CHECK (capacity >= 0),
	closes_at  INTEGER,
	updated_at INTEGER NOT NULL
);

-- Inscriptions en attente de vérification : ce ne sont pas des participants.
-- name_key : clé d'unicité de participants.NormalizeName ; email_key : adresse
-- en minuscules. Une inscription expire à expires_at (48 heures) et est alors
-- purgée. AUTOINCREMENT : un identifiant n'est jamais réattribué, un code
-- resté en base ne peut donc pas passer à une autre inscription.
CREATE TABLE registration_pending (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id       INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	ref            TEXT    NOT NULL CHECK (ref <> ''),
	first_name     TEXT    NOT NULL CHECK (first_name <> ''),
	name           TEXT    NOT NULL CHECK (name <> ''),
	name_key       TEXT    NOT NULL CHECK (name_key <> ''),
	email          TEXT    NOT NULL CHECK (email <> ''),
	email_key      TEXT    NOT NULL CHECK (email_key <> ''),
	shared_address INTEGER NOT NULL DEFAULT 0 CHECK (shared_address IN (0, 1)),
	lang           TEXT    NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	expires_at     INTEGER NOT NULL CHECK (expires_at > created_at)
);

CREATE INDEX registration_pending_group ON registration_pending (group_id, expires_at);
CREATE INDEX registration_pending_name ON registration_pending (group_id, name_key);
CREATE INDEX registration_pending_email ON registration_pending (group_id, email_key);
CREATE INDEX registration_pending_ref ON registration_pending (group_id, ref);
CREATE INDEX registration_pending_expires ON registration_pending (expires_at);

-- Code de vérification en cours d'une inscription (au plus un). code_hash et
-- token_hash : empreintes hexadécimales (token.Hash) du code et du jeton de
-- confirmation en un clic.
CREATE TABLE registration_codes (
	group_id        INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	registration_id INTEGER NOT NULL,
	code_hash       TEXT    NOT NULL CHECK (length(code_hash) = 64 AND code_hash NOT GLOB '*[^0-9a-f]*'),
	token_hash      TEXT    NOT NULL CHECK (length(token_hash) = 64 AND token_hash NOT GLOB '*[^0-9a-f]*'),
	issued_at       INTEGER NOT NULL,
	expires_at      INTEGER NOT NULL,
	attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
	consumed        INTEGER NOT NULL DEFAULT 0 CHECK (consumed IN (0, 1)),
	PRIMARY KEY (group_id, registration_id)
);

CREATE INDEX registration_codes_token ON registration_codes (group_id, token_hash);
