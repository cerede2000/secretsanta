-- État propre à la vérification d'une inscription (F11.4) : traces des
-- inscriptions vérifiées, inscriptions « ignorées » avant le tirage et journal
-- des actions de l'administrateur. S'applique après 001 (groups).
-- Instants en nanosecondes Unix (UTC). Aucun secret, aucune paire.
--
-- Aucune clé étrangère vers registration_pending ni vers participants : la
-- trace d'une inscription vérifiée survit à la ligne d'attente (retirée à la
-- vérification) et au participant (qui peut être retiré du tirage). Elle part
-- avec son groupe, ou à l'expiration d'origine de l'inscription (purge).

-- Trace d'une inscription vérifiée (par code, lien ou administrateur) : rend
-- la vérification idempotente jusqu'à l'expiration de l'inscription.
CREATE TABLE registration_outcomes (
	group_id        INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	registration_id INTEGER NOT NULL,
	ref             TEXT    NOT NULL CHECK (ref <> ''),
	participant_id  INTEGER NOT NULL,
	verified_at     INTEGER NOT NULL,
	by_admin        INTEGER NOT NULL DEFAULT 0 CHECK (by_admin IN (0, 1)),
	expires_at      INTEGER NOT NULL,
	PRIMARY KEY (group_id, registration_id)
);
CREATE INDEX registration_outcomes_ref ON registration_outcomes (group_id, ref);
CREATE INDEX registration_outcomes_expires ON registration_outcomes (expires_at);

-- Inscriptions « ignorées » (fermées, sans code valable, non comptées).
CREATE TABLE registration_ignored (
	group_id        INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	registration_id INTEGER NOT NULL,
	ignored_at      INTEGER NOT NULL,
	PRIMARY KEY (group_id, registration_id)
);

-- Journal des actions de l'administrateur sur les inscriptions : qui (sa
-- nature), quoi, quand, quelle inscription. Jamais de paire.
CREATE TABLE registration_audit (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id        INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	registration_id INTEGER NOT NULL,
	action          TEXT    NOT NULL CHECK (action <> ''),
	actor           TEXT    NOT NULL,
	at              INTEGER NOT NULL,
	first_name      TEXT    NOT NULL,
	name            TEXT    NOT NULL,
	participant_id  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX registration_audit_group ON registration_audit (group_id, at, id);
