-- Outils d'exploitation (F10b.2) : instant de la dernière sauvegarde
-- téléchargée par l'administrateur du groupe (UTC, RFC 3339). Une ligne
-- absente veut dire : aucune sauvegarde.
CREATE TABLE adminops_backups (
	group_id INTEGER PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
	last_at TEXT NOT NULL
);
