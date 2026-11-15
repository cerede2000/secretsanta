-- Liste de suppression (F6c.2) : adresses refusées définitivement (SMTP 5xx),
-- par groupe. L'adresse est stockée en minuscules ; une adresse corrigée est
-- une autre clé et n'est donc jamais supprimée. L'instant est en nanosecondes
-- Unix (UTC).
CREATE TABLE mail_suppression (
	group_id   INTEGER NOT NULL,
	address    TEXT    NOT NULL CHECK (address <> ''),
	reason     TEXT    NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	PRIMARY KEY (group_id, address)
) WITHOUT ROWID;
