-- Rôle dans le tirage (un seul champ, valeurs mutuellement exclusives) et
-- langue du participant. Une ligne préexistante prend le rôle « normal »
-- et aucune langue (= langue par défaut du groupe).
ALTER TABLE participants ADD COLUMN draw_role TEXT NOT NULL DEFAULT 'normal'
	CHECK (draw_role IN ('normal', 'reception_seule', 'offre_plus', 'un_seul'));
ALTER TABLE participants ADD COLUMN locale TEXT NOT NULL DEFAULT ''
	CHECK (length(locale) <= 35);
