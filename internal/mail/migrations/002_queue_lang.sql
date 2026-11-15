-- Langue du message (vague 11) : elle fixe l'en-tête Content-Language et la
-- balise <html lang> à la fabrication. Vide : langue par défaut de mailmsg.
-- Les messages déjà en file gardent la valeur vide, donc leur comportement
-- d'origine.
ALTER TABLE mail_queue ADD COLUMN lang TEXT NOT NULL DEFAULT '';
