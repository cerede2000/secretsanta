-- Lettre au Père Noël (F5.1). Une lettre par participant (le tiré) ; aucune
-- colonne ne désigne le tireur. version est incrémentée à chaque modification.
-- pending_ideas : idées ajoutées ou modifiées depuis la dernière notification
-- (identifiants séparés par des virgules), remis à zéro par MarkNotified.
CREATE TABLE letters (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	participant_id INTEGER NOT NULL UNIQUE REFERENCES participants(id) ON DELETE CASCADE,
	intro TEXT NOT NULL DEFAULT '' CHECK (length(intro) <= 8000),
	version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
	pending_ideas TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX letters_group ON letters(group_id);

CREATE TABLE letter_ideas (
	id INTEGER PRIMARY KEY,
	letter_id INTEGER NOT NULL REFERENCES letters(id) ON DELETE CASCADE,
	position INTEGER NOT NULL,
	title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 400),
	url TEXT NOT NULL DEFAULT '' CHECK (length(url) <= 2000),
	image_file TEXT NOT NULL DEFAULT '' CHECK (length(image_file) <= 200),
	image_type TEXT NOT NULL DEFAULT '' CHECK (image_type IN ('', 'image/jpeg', 'image/png')),
	body TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 8000),
	updated_version INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX letter_ideas_letter ON letter_ideas(letter_id, position);

-- Plafond de 5 idées garanti aussi par la base, même en concurrence.
CREATE TRIGGER letter_ideas_max5 BEFORE INSERT ON letter_ideas
WHEN (SELECT count(*) FROM letter_ideas WHERE letter_id = NEW.letter_id) >= 5
BEGIN
	SELECT RAISE(ABORT, 'letter_ideas_max');
END;

-- Historique des versions : idées touchées par chaque modification.
CREATE TABLE letter_versions (
	letter_id INTEGER NOT NULL REFERENCES letters(id) ON DELETE CASCADE,
	version INTEGER NOT NULL,
	changed_at TEXT NOT NULL,
	changed_ideas TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (letter_id, version)
);

-- Cachet de lecture (utilisé par F5b) : dernière version lue.
CREATE TABLE letter_reads (
	letter_id INTEGER PRIMARY KEY REFERENCES letters(id) ON DELETE CASCADE,
	version_read INTEGER NOT NULL,
	read_at TEXT NOT NULL
);

-- Archives (utilisées par F5b) : instantané d'une lettre à une version.
CREATE TABLE letter_archives (
	id INTEGER PRIMARY KEY,
	letter_id INTEGER NOT NULL REFERENCES letters(id) ON DELETE CASCADE,
	version INTEGER NOT NULL,
	snapshot TEXT NOT NULL,
	archived_at TEXT NOT NULL
);
CREATE INDEX letter_archives_letter ON letter_archives(letter_id);
