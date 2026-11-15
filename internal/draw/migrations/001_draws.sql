-- Tirages persistants (F4b.1) : un tirage par année et par groupe, un seul
-- tirage « actuel » à la fois, les anciens conservés en « archive ».
-- Aucune clé étrangère vers les participants : leur table appartient à un
-- autre module et l'historique doit survivre à leurs changements.
CREATE TABLE draws (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	year INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
	kind TEXT NOT NULL CHECK (kind IN ('tirage', 'tirage_refait')),
	status TEXT NOT NULL CHECK (status IN ('actuel', 'archive')),
	revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
	gift_cap INTEGER NOT NULL CHECK (gift_cap >= 1),
	drawn_at TEXT NOT NULL,
	archived_at TEXT,
	CHECK ((status = 'archive') = (archived_at IS NOT NULL))
);
-- Un seul tirage actuel par groupe, même en concurrence.
CREATE UNIQUE INDEX draws_one_current ON draws(group_id) WHERE status = 'actuel';
CREATE INDEX draws_group_year ON draws(group_id, year);

-- Un tirage archivé ne redevient jamais actuel.
CREATE TRIGGER draws_archive_final
BEFORE UPDATE OF status ON draws
WHEN OLD.status = 'archive' AND NEW.status IS NOT 'archive'
BEGIN
	SELECT RAISE(ABORT, 'draws : un tirage archivé est définitif');
END;

-- Une ligne par cible : chaque participant reçoit exactement un cadeau
-- (UNIQUE draw_id, receiver_id) ; un tireur peut avoir plusieurs lignes.
-- discovered_at est le drapeau serveur de découverte, un par cible ; un
-- nouveau tirage crée de nouvelles lignes, donc des drapeaux réarmés.
CREATE TABLE draw_pairs (
	id INTEGER PRIMARY KEY,
	draw_id INTEGER NOT NULL REFERENCES draws(id) ON DELETE CASCADE,
	giver_id INTEGER NOT NULL,
	receiver_id INTEGER NOT NULL,
	discovered_at TEXT,
	CHECK (giver_id <> receiver_id),
	UNIQUE (draw_id, receiver_id)
);
CREATE INDEX draw_pairs_giver ON draw_pairs(draw_id, giver_id);

-- Une paire ne change jamais de tireur ni de destinataire : un ajustement
-- supprime et réinsère.
CREATE TRIGGER draw_pairs_immutable
BEFORE UPDATE OF draw_id, giver_id, receiver_id ON draw_pairs
BEGIN
	SELECT RAISE(ABORT, 'draw_pairs : une paire est immuable');
END;

-- Une découverte ne revient jamais en arrière.
CREATE TRIGGER draw_pairs_discovery_final
BEFORE UPDATE OF discovered_at ON draw_pairs
WHEN OLD.discovered_at IS NOT NULL AND NEW.discovered_at IS NOT OLD.discovered_at
BEGIN
	SELECT RAISE(ABORT, 'draw_pairs : une découverte est définitive');
END;

-- Un tirage archivé ne reçoit plus de paire.
CREATE TRIGGER draw_pairs_no_insert_archived
BEFORE INSERT ON draw_pairs
WHEN (SELECT status FROM draws WHERE id = NEW.draw_id) = 'archive'
BEGIN
	SELECT RAISE(ABORT, 'draw_pairs : tirage archivé');
END;

-- Journal de l'historique : tirage, tirage refait, archivage, ajustement.
-- Aucun nom, aucune paire.
CREATE TABLE draw_events (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	draw_id INTEGER NOT NULL REFERENCES draws(id) ON DELETE CASCADE,
	kind TEXT NOT NULL CHECK (kind IN ('tirage', 'tirage_refait', 'archivage', 'ajustement')),
	revision INTEGER NOT NULL CHECK (revision >= 1),
	at TEXT NOT NULL
);
CREATE INDEX draw_events_draw ON draw_events(draw_id, id);
