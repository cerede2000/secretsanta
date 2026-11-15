package mailsuppress

import (
	"secretsanta/internal/db"
	"secretsanta/internal/mailsuppress/migrations"
)

// Module est le nom de module des migrations de la liste de suppression.
const Module = migrations.Module

// Migrations renvoie les migrations à enregistrer par l'orchestrateur.
func Migrations() ([]db.Migration, error) { return migrations.Migrations() }
