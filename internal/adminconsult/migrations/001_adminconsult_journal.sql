-- Journal des consultations de paire (F10b.1), visible de tous les
-- participants du groupe. Une ligne dit seulement QUAND l'organisateur a
-- consulté une paire : aucune colonne ne désigne la personne consultée ni sa
-- cible, aucun texte libre. Les instants sont en nanosecondes Unix (UTC).
CREATE TABLE adminconsult_journal (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	at       INTEGER NOT NULL
);
CREATE INDEX adminconsult_journal_group ON adminconsult_journal (group_id, id);

-- Le journal est en ajout seul : une consultation consignée ne s'efface ni
-- ne se réécrit (seule la suppression du groupe l'emporte, par cascade).
CREATE TRIGGER adminconsult_journal_no_update
BEFORE UPDATE ON adminconsult_journal
BEGIN
	SELECT RAISE(ABORT, 'adminconsult_journal : le journal est en ajout seul');
END;
