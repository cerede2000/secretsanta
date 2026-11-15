-- Réglages des relances par groupe et garde-fou « un courriel par jour » (F7.1).
-- Une ligne absente veut dire : valeurs par défaut (3 puis 7 jours, plafond 3).
CREATE TABLE reminder_group_rules (
	group_id INTEGER PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
	enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
	first_delay_days INTEGER NOT NULL DEFAULT 3 CHECK (first_delay_days BETWEEN 1 AND 30),
	spacing_days INTEGER NOT NULL DEFAULT 4 CHECK (spacing_days BETWEEN 1 AND 30),
	max_per_type INTEGER NOT NULL DEFAULT 3 CHECK (max_per_type BETWEEN 1 AND 5)
);

-- Un courriel de relance par personne et par jour local, tous types confondus :
-- la clé unique (groupe, personne, jour) rend la réservation atomique.
CREATE TABLE reminder_daily_sends (
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	participant_id INTEGER NOT NULL,
	sent_on TEXT NOT NULL CHECK (sent_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
	kind TEXT NOT NULL,
	claimed_at TEXT NOT NULL,
	PRIMARY KEY (group_id, participant_id, sent_on)
);
