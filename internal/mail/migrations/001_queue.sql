-- File d'envoi des courriels (F6.2). Les instants sont en nanosecondes Unix (UTC).
-- Les états publics sont pending, sent et failed ; sending est le verrou
-- anti-doublon posé par l'envoyeur qui a réservé le message (lock_token).
CREATE TABLE mail_queue (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id        INTEGER NOT NULL DEFAULT 0,
	participant_id  INTEGER NOT NULL DEFAULT 0,
	recipient       TEXT    NOT NULL,
	recipient_name  TEXT    NOT NULL DEFAULT '',
	mail_type       TEXT    NOT NULL,
	essential       INTEGER NOT NULL DEFAULT 0 CHECK (essential IN (0, 1)),
	subject         TEXT    NOT NULL DEFAULT '',
	body_text       TEXT    NOT NULL DEFAULT '',
	body_html       TEXT    NOT NULL DEFAULT '',
	unsubscribe_url TEXT    NOT NULL DEFAULT '',
	coalesce_key    TEXT    NOT NULL DEFAULT '',
	status          TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
	attempts        INTEGER NOT NULL DEFAULT 0,
	last_error      TEXT    NOT NULL DEFAULT '',
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL,
	send_after      INTEGER NOT NULL,
	locked_at       INTEGER,
	lock_token      TEXT    NOT NULL DEFAULT '',
	sent_at         INTEGER,
	failed_at       INTEGER
);

CREATE INDEX mail_queue_due ON mail_queue (status, send_after);
CREATE INDEX mail_queue_participant ON mail_queue (group_id, participant_id);

-- Au plus un message en attente par clé de regroupement.
CREATE UNIQUE INDEX mail_queue_coalesce ON mail_queue (coalesce_key)
	WHERE coalesce_key <> '' AND status = 'pending';
