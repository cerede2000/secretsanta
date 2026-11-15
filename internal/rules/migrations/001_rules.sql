-- Foyers, exclusions manuelles et forçages (F3.2).
-- Aucune clé étrangère vers les participants : leur table appartient à un
-- autre module ; le service vérifie l'appartenance au groupe.
CREATE TABLE households (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80)
);

-- participant_id est la clé primaire : un participant n'a qu'un seul foyer.
CREATE TABLE household_members (
	participant_id INTEGER PRIMARY KEY,
	household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE
);

CREATE TABLE manual_exclusions (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	giver_id INTEGER NOT NULL,
	receiver_id INTEGER NOT NULL,
	CHECK (giver_id <> receiver_id),
	UNIQUE (group_id, giver_id, receiver_id)
);

CREATE TABLE forced_pairs (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	giver_id INTEGER NOT NULL,
	receiver_id INTEGER NOT NULL,
	CHECK (giver_id <> receiver_id),
	UNIQUE (group_id, giver_id, receiver_id)
);
