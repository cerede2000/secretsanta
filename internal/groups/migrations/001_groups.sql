-- Groupes Secret Santa (F2.1).
-- Le slug est immuable : un déclencheur refuse toute modification.
-- Le budget est facultatif : NULL signifie « aucun budget ».
-- Le secret d'administration n'est jamais stocké en clair : seul son
-- condensé SHA-256 (32 octets) est conservé.
CREATE TABLE groups (
	id INTEGER PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE
		CHECK (length(slug) BETWEEN 3 AND 40 AND slug NOT GLOB '*[^a-z0-9-]*'),
	name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
	budget_cents INTEGER
		CHECK (budget_cents IS NULL OR budget_cents BETWEEN 100 AND 1000000),
	budget_version INTEGER NOT NULL DEFAULT 0 CHECK (budget_version >= 0),
	gift_instructions TEXT NOT NULL DEFAULT '' CHECK (length(gift_instructions) <= 1000),
	event_date TEXT,
	reveal_at TEXT,
	admin_can_view_pair INTEGER NOT NULL DEFAULT 0 CHECK (admin_can_view_pair IN (0, 1)),
	max_gifts_per_giver INTEGER NOT NULL DEFAULT 2 CHECK (max_gifts_per_giver BETWEEN 2 AND 5),
	admin_secret_hash BLOB NOT NULL CHECK (length(admin_secret_hash) = 32),
	created_at TEXT NOT NULL
);

CREATE TRIGGER groups_slug_immutable
BEFORE UPDATE OF slug ON groups
WHEN NEW.slug IS NOT OLD.slug
BEGIN
	SELECT RAISE(ABORT, 'groups.slug est immuable');
END;
