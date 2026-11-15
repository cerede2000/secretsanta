-- Participants d'un groupe (F3.1).
-- name_key est la clé d'unicité du nom (sans casse ni accents), calculée par
-- NormalizeName ; la contrainte UNIQUE garantit l'unicité même en concurrence.
-- link_hash et code_hash sont posés par F2 / F3d ; jamais modifiés ici.
-- Le courriel n'est pas unique : plusieurs participants peuvent le partager.
CREATE TABLE participants (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
	first_name TEXT NOT NULL CHECK (length(first_name) BETWEEN 1 AND 80),
	name_key TEXT NOT NULL CHECK (length(name_key) > 0),
	email TEXT NOT NULL DEFAULT '' CHECK (length(email) <= 254),
	gift_hint TEXT NOT NULL DEFAULT '' CHECK (length(gift_hint) <= 1000),
	link_hash BLOB,
	code_hash BLOB,
	joined_at TEXT NOT NULL,
	UNIQUE (group_id, name_key)
);
CREATE INDEX participants_group ON participants(group_id);
