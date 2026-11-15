-- Désinscription par type de mail (F6.4). Une ligne = un participant coupé
-- d'un type non essentiel. Les instants sont en nanosecondes Unix (UTC).
CREATE TABLE mail_unsubscribe (
	participant_id INTEGER NOT NULL,
	mail_type      TEXT    NOT NULL CHECK (mail_type IN ('relances', 'nouvelle_idee')),
	group_id       INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL,
	PRIMARY KEY (participant_id, mail_type)
);
