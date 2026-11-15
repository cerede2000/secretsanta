-- Changements (règle ou rôle) enregistrés après le tirage sans réparation :
-- ils ne valent que pour les futurs ajustements et les nouveaux tirages.
-- Pas de clé étrangère vers les tables des autres modules (sauf groups).
--   kind  : exclusion, forcage, foyer ou role
--   a_id  : tireur (exclusion, forçage), foyer (foyer) ou participant (rôle)
--   b_id  : bénéficiaire (exclusion, forçage), membre ajouté (foyer, 0 si création), 0 (rôle)
CREATE TABLE unaffected_changes (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	kind TEXT NOT NULL CHECK (kind IN ('exclusion', 'forcage', 'foyer', 'role')),
	a_id INTEGER NOT NULL,
	b_id INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	UNIQUE (group_id, kind, a_id, b_id)
);
