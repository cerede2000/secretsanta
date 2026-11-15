-- Journal des actions sensibles de l'administrateur (F10.1) et marque
-- « lien remis en main propre » des personnes sans courriel.
--
-- Le journal ne stocke jamais de paire ni de jeton : ni colonne de texte
-- libre, ni cible. Une ligne dit seulement qui (la personne concernée, s'il y
-- en a une), quoi (un code d'action) et quand. Les instants sont en
-- nanosecondes Unix (UTC).
CREATE TABLE admin_audit (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id       INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	action         TEXT    NOT NULL CHECK (length(action) BETWEEN 1 AND 64 AND action NOT GLOB '*[^a-z0-9_]*'),
	participant_id INTEGER,
	at             INTEGER NOT NULL
);
CREATE INDEX admin_audit_group ON admin_audit (group_id, id);

-- Une ligne = le lien d'une personne sans courriel a été remis en main propre.
CREATE TABLE admin_link_handover (
	group_id       INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	participant_id INTEGER NOT NULL,
	handed_at      INTEGER NOT NULL,
	PRIMARY KEY (group_id, participant_id)
);
