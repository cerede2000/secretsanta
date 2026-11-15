-- Accès participant (F2b) : jetons de lien personnel (kind = 'link') et codes
-- courts (kind = 'code'). Seule l'empreinte HMAC-SHA-256 (hexadécimal, clé de
-- signature de l'instance) est conservée, jamais le secret en clair.
CREATE TABLE access_credentials (
	group_id       TEXT NOT NULL CHECK (group_id <> ''),
	participant_id TEXT NOT NULL CHECK (participant_id <> ''),
	kind           TEXT NOT NULL CHECK (kind IN ('link', 'code')),
	token_hash     TEXT NOT NULL CHECK (length(token_hash) = 64 AND token_hash NOT GLOB '*[^0-9a-f]*')
);

-- Une empreinte n'appartient qu'à un participant par groupe et par nature
-- (code court unique dans le groupe) ; c'est aussi l'index de recherche.
CREATE UNIQUE INDEX access_credentials_hash ON access_credentials (group_id, kind, token_hash);

-- Un seul secret actif par participant et par nature : en émettre un nouveau
-- remplace le précédent.
CREATE UNIQUE INDEX access_credentials_participant ON access_credentials (group_id, kind, participant_id);
