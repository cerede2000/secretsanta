-- Révélation (F4b.2). S'applique après les migrations de groups et de draw.
-- Aucune table ne contient de nom ni de paire : seulement des identifiants de
-- tirage, de cible (ligne de draw_pairs) et des nombres.

-- Une ligne par groupe, écrite en tête de chaque transaction de la
-- révélation : elle prend le verrou d'écriture avant toute lecture, ce qui
-- sérialise la modification de la date et l'ouverture.
CREATE TABLE reveal_groups (
	group_id INTEGER PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
	changes INTEGER NOT NULL DEFAULT 0 CHECK (changes >= 0)
);

-- Ouverture de la révélation d'un tirage : à la date, sans date (immédiate)
-- ou par « ouvrir maintenant ». Un nouveau tirage a un nouvel identifiant,
-- donc une révélation à rouvrir.
CREATE TABLE reveal_openings (
	draw_id INTEGER PRIMARY KEY REFERENCES draws(id) ON DELETE CASCADE,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	how TEXT NOT NULL CHECK (how IN ('date', 'immediat', 'manuel')),
	opened_at TEXT NOT NULL
);

-- Une ouverture ne revient jamais en arrière.
CREATE TRIGGER reveal_openings_final
BEFORE UPDATE ON reveal_openings
BEGIN
	SELECT RAISE(ABORT, 'reveal_openings : une ouverture est définitive');
END;

-- Ouverture par cible : une ligne par cible ouverte, posée une seule fois.
-- Une cible ajoutée plus tard (ajustement) n'a pas de ligne : la tâche
-- planifiée n'ouvre que celles-là.
CREATE TABLE reveal_targets (
	pair_id INTEGER PRIMARY KEY REFERENCES draw_pairs(id) ON DELETE CASCADE,
	draw_id INTEGER NOT NULL REFERENCES draws(id) ON DELETE CASCADE,
	opened_at TEXT NOT NULL
);

CREATE TRIGGER reveal_targets_final
BEFORE UPDATE ON reveal_targets
BEGIN
	SELECT RAISE(ABORT, 'reveal_targets : une ouverture est définitive');
END;

-- File sortante du courriel « ton tirage est prêt » : un avis par personne
-- et par tirage, avec seulement son nombre de cibles (0, 1 ou 2).
CREATE TABLE reveal_notices (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	draw_id INTEGER NOT NULL REFERENCES draws(id) ON DELETE CASCADE,
	participant_id INTEGER NOT NULL,
	targets INTEGER NOT NULL CHECK (targets BETWEEN 0 AND 5),
	created_at TEXT NOT NULL,
	sent_at TEXT,
	UNIQUE (draw_id, participant_id)
);
CREATE INDEX reveal_notices_pending ON reveal_notices(sent_at, id);
