-- Lecture de la lettre par le tireur (F5b). S'applique après les migrations
-- de letter et de draw.
--
-- Cachet « lue par le Père Noël » : une ligne par lettre, la dernière version
-- ouverte par le tireur de la cible (pair_id, ligne de draw_pairs). Le cachet
-- ne vaut que si la version lue est la version actuelle de la lettre ET si
-- la cible lue est encore la cible actuelle du tiré : une nouvelle version le
-- retire, un ajustement (la paire est supprimée, la ligne part avec elle) ou
-- un nouveau tirage (nouvelles paires) aussi. Aucune date, aucun nom.
CREATE TABLE letterread_stamps (
	letter_id INTEGER PRIMARY KEY REFERENCES letters(id) ON DELETE CASCADE,
	pair_id INTEGER NOT NULL REFERENCES draw_pairs(id) ON DELETE CASCADE,
	version INTEGER NOT NULL CHECK (version >= 1)
);
CREATE INDEX letterread_stamps_pair ON letterread_stamps(pair_id);
