-- Choix du rôle à l'inscription (F11d). Les deux colonnes ont une valeur par
-- défaut : les lignes existantes gardent le comportement d'avant (option
-- désactivée, rôle normal). S'applique après 001 (invitation, inscriptions en
-- attente).

-- « Proposer aux inscrits de choisir leur rôle » : désactivé par défaut,
-- modifiable à tout moment par l'administrateur.
ALTER TABLE registration_invitations
	ADD COLUMN role_choice INTEGER NOT NULL DEFAULT 0 CHECK (role_choice IN (0, 1));

-- Rôle choisi par la personne, transmis au participant créé à la vérification.
-- Jamais un_seul : ce rôle n'est pas permis à l'inscription.
ALTER TABLE registration_pending
	ADD COLUMN role TEXT NOT NULL DEFAULT 'normal' CHECK (role IN ('normal', 'reception_seule', 'offre_plus'));
