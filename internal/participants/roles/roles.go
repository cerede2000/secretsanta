// Package roles porte la règle pure de réglage du rôle dans le tirage (F3.3) :
// qui peut passer de quel rôle à quel rôle. Aucun accès base : le service
// participants applique les mêmes règles ; ce paquet les rend testables seules.
package roles

import (
	"secretsanta/internal/participants"
)

// Verdict est le résultat d'une demande de changement de rôle.
type Verdict int

const (
	Allowed Verdict = iota
	// UnknownRole : la valeur demandée n'existe pas.
	UnknownRole
	// LockedAfterDraw : la personne ne peut plus changer sa préférence.
	LockedAfterDraw
	// SetByAdmin : le rôle actuel a été fixé par l'administrateur.
	SetByAdmin
	// SelfValueOnly : la personne ne peut choisir que normal ou un_seul.
	SelfValueOnly
)

// Explain renvoie l'explication en français d'un refus (vide si Allowed).
func (v Verdict) Explain() string {
	switch v {
	case UnknownRole:
		return "Ce rôle dans le tirage n'existe pas."
	case LockedAfterDraw:
		return "Le tirage est fait : la préférence ne peut plus être changée par la personne elle-même ; contactez l'administrateur."
	case SetByAdmin:
		return "Ce rôle a été fixé par l'administrateur : seul l'administrateur peut le changer."
	case SelfValueOnly:
		return "Tu peux seulement choisir entre offrir normalement et n'offrir qu'un seul cadeau."
	}
	return ""
}

// AdminMayChange : l'administrateur peut tout régler, à tout moment.
func AdminMayChange(to participants.DrawRole) Verdict {
	if !to.Valid() {
		return UnknownRole
	}
	return Allowed
}

// SelfMayChange : la personne, avant le tirage seulement, entre normal et un_seul,
// et seulement si son rôle actuel est l'un des deux.
func SelfMayChange(from, to participants.DrawRole, drawDone bool) Verdict {
	if !to.Valid() {
		return UnknownRole
	}
	if drawDone {
		return LockedAfterDraw
	}
	if from != participants.RoleNormal && from != participants.RoleUnSeul {
		return SetByAdmin
	}
	if to != participants.RoleNormal && to != participants.RoleUnSeul {
		return SelfValueOnly
	}
	return Allowed
}

// Counts sont les décomptes sans nom d'une liste de rôles : bénéficiaires (tous),
// tireurs (tous sauf reception_seule) et chaque rôle.
func Counts(list []participants.DrawRole) participants.RoleCounts {
	var c participants.RoleCounts
	for _, r := range list {
		c.Beneficiaries++
		switch r {
		case participants.RoleReceptionSeule:
			c.ReceptionSeule++
		case participants.RoleOffrePlus:
			c.Givers++
			c.OffrePlus++
		case participants.RoleUnSeul:
			c.Givers++
			c.UnSeul++
		default:
			c.Givers++
			c.Normal++
		}
	}
	return c
}
