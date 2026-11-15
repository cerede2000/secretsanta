-- Options du tirage (vague 11) : « exclure le tirage de l'année précédente » et
-- « anti-échange A <-> B ». Une ligne par groupe ; une ligne absente veut dire
-- que les deux options sont désactivées (comportement d'origine). Les options
-- ne valent que pour les tirages à venir : elles ne modifient jamais une paire.
CREATE TABLE group_draw_options (
	group_id INTEGER PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
	exclude_previous_year INTEGER NOT NULL DEFAULT 0 CHECK (exclude_previous_year IN (0, 1)),
	anti_swap INTEGER NOT NULL DEFAULT 0 CHECK (anti_swap IN (0, 1))
);
