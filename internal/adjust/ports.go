package adjust

import (
	"context"
	"database/sql"

	"secretsanta/internal/draw"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/ports"
)

// DrawWriter remplace les paires du tirage actuel dans la transaction de
// l'appelant, en gardant les drapeaux des paires inchangées et en consignant
// l'ajustement dans l'historique (satisfait par *draw.Service).
type DrawWriter interface {
	AdjustTx(ctx context.Context, tx *sql.Tx, groupID int64, pairs []draw.Pair) error
}

// Archiver archive, pour un tireur, la lettre et les messages de son
// ancienne cible (port fourni par F5b, branché par l'orchestrateur). Il est
// appelé dans la transaction de l'ajustement, avant le remplacement des
// paires et avant tout retrait de participant : un échec annule tout.
type Archiver interface {
	ArchiveTx(ctx context.Context, tx *sql.Tx, groupID, giverID, oldReceiverID int64) error
}

// Remover retire un participant du groupe dans la transaction de
// l'ajustement (ligne du participant et règles qui le citent). Il est appelé
// après l'archivage et le remplacement des paires. Fourni par
// l'orchestrateur : le participant appartient à un autre module.
type Remover interface {
	RemoveTx(ctx context.Context, tx *sql.Tx, groupID, participantID int64) error
}

// Contact est ce qu'il faut pour prévenir une personne : son adresse, son
// prénom et sa langue. Jamais une autre personne.
type Contact struct {
	Email     string
	FirstName string
	Locale    string
}

// Directory lit le contact d'un participant.
type Directory interface {
	Contact(ctx context.Context, groupID, participantID int64) (Contact, error)
}

// ParticipantDirectory adapte *participants.Service au port Directory.
type ParticipantDirectory struct{ Participants *participants.Service }

// Contact lit le participant avec les droits de l'administrateur du groupe.
func (d ParticipantDirectory) Contact(ctx context.Context, groupID, participantID int64) (Contact, error) {
	p, err := d.Participants.Get(ctx, participants.AdminActor(groupID), groupID, participantID)
	if err != nil {
		return Contact{}, err
	}
	return Contact{Email: p.Email, FirstName: p.FirstName, Locale: p.Locale}, nil
}

// Notice est l'avis à envoyer à une personne dont les cibles changent. Il
// ne porte que le destinataire : jamais sa cible, ni l'ancienne, ni la
// personne retirée ou ajoutée.
type Notice struct {
	GroupID       int64
	ParticipantID int64
	Contact       Contact
	// NewTarget est vrai quand la personne a une nouvelle cible à découvrir ;
	// faux quand elle a seulement une cible en moins.
	NewTarget bool
}

// Composer écrit le courriel d'un avis. L'orchestrateur peut brancher les
// gabarits (templates.DrawAdjusted, lien personnel) ; DefaultComposer sert
// sinon.
type Composer func(ctx context.Context, n Notice) (ports.EmailMessage, error)

// EmailTypeDrawAdjusted est la nature du courriel d'ajustement (même valeur
// que templates.DrawAdjusted).
const EmailTypeDrawAdjusted ports.EmailType = "draw_adjusted"

// DefaultComposer écrit un courriel français qui ne nomme que le
// destinataire.
func DefaultComposer(_ context.Context, n Notice) (ports.EmailMessage, error) {
	body := mailGreeting(n.Contact.FirstName) + "\n\n"
	if n.NewTarget {
		body += mailNewTarget
	} else {
		body += mailFewerTargets
	}
	body += "\n\n" + mailDiscretion
	return ports.EmailMessage{
		To:        n.Contact.Email,
		Subject:   mailSubject,
		Body:      body,
		Type:      EmailTypeDrawAdjusted,
		Essential: true,
	}, nil
}
