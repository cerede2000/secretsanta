-- Archives de lettres des personnes retirées du tirage (ajustement du tirage).
--
-- Retirer une personne supprime sa ligne dans participants, ce qui fait
-- disparaître en cascade sa lettre puis les archives de sa lettre
-- (letter_archives). Pour que l'archive survive, le retrait en copie les
-- instantanés ici avant de supprimer la personne.
--
-- Aucune clé étrangère vers participants, letters ni letter_archives : la
-- ligne doit survivre à leur suppression. Seule la clé vers groups reste (un
-- groupe supprimé emporte ses archives). participant_id n'est qu'un repère : un
-- identifiant peut être réattribué à une personne ajoutée plus tard. Aucun nom,
-- aucune adresse, aucune paire : seulement l'instantané de la lettre, qui ne
-- contient jamais le tireur. Cette table n'est jamais servie à un tireur.
CREATE TABLE removed_letter_archives (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id       INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	participant_id INTEGER NOT NULL,
	version        INTEGER NOT NULL CHECK (version >= 1),
	snapshot       TEXT    NOT NULL,
	archived_at    TEXT    NOT NULL,
	removed_at     TEXT    NOT NULL
);
CREATE INDEX removed_letter_archives_group ON removed_letter_archives (group_id, participant_id);
