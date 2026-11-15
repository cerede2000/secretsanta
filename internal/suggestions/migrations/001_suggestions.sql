-- Suggestions de cadeaux (F8c.1). Le cache est propre à chaque cible (tireur +
-- tiré) et indexé par budget (montant et version) et par mots-clés : un budget
-- modifié, retiré ou redéfini ne réutilise jamais l'entrée de l'ancien budget.
-- Aucune colonne ne contient de nom, de courriel ni de mot de la lettre : seul
-- l'empreinte des mots-clés est gardée.
CREATE TABLE suggestion_cache (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	giver_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	receiver_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	budget_key TEXT NOT NULL,
	keywords_key TEXT NOT NULL,
	lang TEXT NOT NULL,
	items TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	UNIQUE (group_id, giver_id, receiver_id, budget_key, keywords_key, lang)
);
CREATE INDEX suggestion_cache_fetched ON suggestion_cache(fetched_at);

-- Une ligne par recherche réellement envoyée aux sources (quotas).
CREATE TABLE suggestion_searches (
	id INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	giver_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	at TEXT NOT NULL
);
CREATE INDEX suggestion_searches_giver ON suggestion_searches(group_id, giver_id, at);
CREATE INDEX suggestion_searches_group ON suggestion_searches(group_id, at);
