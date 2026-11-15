-- Langue par défaut du groupe (F2e). Une ligne absente veut dire : langue
-- par défaut de l'instance.
CREATE TABLE group_settings (
	group_id INTEGER PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
	default_lang TEXT NOT NULL CHECK (length(default_lang) BETWEEN 2 AND 35)
);
