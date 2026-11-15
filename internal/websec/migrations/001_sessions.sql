CREATE TABLE websec_sessions (
	id_hash    TEXT    NOT NULL PRIMARY KEY,
	kind       TEXT    NOT NULL CHECK (kind IN ('admin', 'participant')),
	group_id   TEXT    NOT NULL CHECK (group_id <> ''),
	subject    TEXT    NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);

CREATE INDEX websec_sessions_expires_at ON websec_sessions (expires_at);
