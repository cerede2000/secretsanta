// Package letter porte la lettre au Père Noël (F5) : idées plafonnées à 5,
// images téléversées en local, numéro de version et événement regroupable,
// sans jamais porter l'identité du tireur.
package letter

import (
	"embed"
	"time"

	"secretsanta/internal/db"
)

// Module est le nom du module dans schema_migrations.
const Module = "letter"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations renvoie les migrations embarquées, à enregistrer par l'orchestrateur
// (après groups et participants).
func Migrations() ([]db.Migration, error) {
	return db.LoadFS(migrationsFS, "migrations")
}

// Bornes des saisies.
const (
	MaxIdeas      = 5
	IntroMax      = 2000
	TitleMax      = 100
	URLMax        = 500
	TextMax       = 1000
	ImageMaxBytes = 2 << 20
	ImageMaxSide  = 4000
)

// Contrat de l'événement « lettre nouvelle ou modifiée » consommé par F7.
const (
	// EventUpdated est le nom de l'événement.
	EventUpdated = "letter.updated"
	// CoalesceQuiet : attente après la dernière modification avant d'envoyer.
	CoalesceQuiet = 10 * time.Minute
	// CoalesceCap : délai maximal depuis la première modification non notifiée.
	CoalesceCap = 60 * time.Minute

	// Clés de la charge utile (Event.Payload).
	PayloadKey         = "coalesce_key"  // clé de regroupement stable : "letter:<id>"
	PayloadLetterID    = "letter_id"     // identifiant de la lettre
	PayloadRecipientID = "recipient_id"  // le tiré, auteur de la lettre
	PayloadVersion     = "version"       // numéro de version après modification
	PayloadAt          = "at"            // horodatage serveur, RFC 3339 UTC
	PayloadIdeas       = "changed_ideas" // idées cumulées depuis la dernière notification (ids, virgules)
)

// Idea est une idée de la lettre, telle que vue par le tiré ou le tireur.
// L'image n'est jamais une URL distante : elle est servie par ImageFile.
type Idea struct {
	ID       int64
	Title    string
	URL      string
	Text     string
	HasImage bool
}

// Letter est la lettre d'un tiré. Elle ne contient jamais le tireur.
type Letter struct {
	ID        int64
	Version   int
	Intro     string
	Ideas     []Idea
	UpdatedAt time.Time
}

// IdeaInput est la saisie brute d'une idée.
type IdeaInput struct {
	Title       string
	URL         string
	Text        string
	Image       []byte // facultatif : contenu téléversé
	RemoveImage bool   // à la modification : retire l'image existante
}

// Notification est le modèle de notification à destination du tireur : il ne
// contient que le nom du tiré concerné.
type Notification struct {
	RecipientFirstName string
	IdeaCount          int
	Version            int
}
