-- Relances (F7.2 à F7.4) : journal des relances envoyées (le plafond par type
-- et par personne se lit ici) et fait « lien ouvert » (posé une seule fois).
CREATE TABLE reminders_sent (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL,
	participant_id INTEGER NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('lien', 'lettre', 'lecture')),
	automatic INTEGER NOT NULL CHECK (automatic IN (0, 1)),
	sent_at TEXT NOT NULL
);
CREATE INDEX reminders_sent_person ON reminders_sent(group_id, participant_id, kind);

CREATE TABLE reminder_link_opens (
	group_id INTEGER NOT NULL,
	participant_id INTEGER NOT NULL,
	opened_at TEXT NOT NULL,
	PRIMARY KEY (group_id, participant_id)
) WITHOUT ROWID;
