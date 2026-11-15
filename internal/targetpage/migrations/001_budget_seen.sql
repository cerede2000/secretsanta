-- Pages de l'attente et de la cible (F4d.2). S'applique après les migrations
-- de groups et de participants.
-- Version du budget vue par chaque participant : le bandeau « Budget mis à
-- jour » s'affiche tant que budget_version du groupe dépasse cette valeur.
-- Aucune paire, aucun nom : seulement des identifiants et un nombre.
CREATE TABLE targetpage_budget_seen (
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	participant_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	version INTEGER NOT NULL CHECK (version >= 0),
	PRIMARY KEY (group_id, participant_id)
);
