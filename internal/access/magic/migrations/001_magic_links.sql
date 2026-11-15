-- Liens magiques de connexion (F2b) : seul le hachage SHA-256 du jeton est
-- stocké ; les instants sont en nanosecondes Unix (UTC).
CREATE TABLE magic_links (
	token_hash     BLOB    NOT NULL PRIMARY KEY,
	group_id       INTEGER NOT NULL,
	participant_id INTEGER NOT NULL,
	created_at     INTEGER NOT NULL,
	expires_at     INTEGER NOT NULL,
	used_at        INTEGER
);

CREATE INDEX magic_links_participant ON magic_links (group_id, participant_id);
CREATE INDEX magic_links_expires ON magic_links (expires_at);
