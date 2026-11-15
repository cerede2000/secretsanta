package drawengine

import (
	"fmt"
	"strings"
)

// Cause classe une impossibilité de tirage.
type Cause string

const (
	CauseInvalidInput         Cause = "entree_invalide"
	CauseTooFewGivers         Cause = "trop_peu_de_tireurs"
	CauseCapNeedsConfirmation Cause = "plafond_a_confirmer"
	CauseTooManyOneGift       Cause = "trop_de_personnes_a_un_cadeau"
	CauseCapTooLow            Cause = "plafond_trop_bas"
	CauseForcedReceptionOnly  Cause = "forcage_reception_seule"
	CauseForcedSelf           Cause = "forcage_soi_meme"
	CauseForcedTwice          Cause = "forcage_double_destinataire"
	CauseForcedConflict       Cause = "forcage_contredit_une_regle"
	CauseForcedSwap           Cause = "forcage_echange"
	CauseForcedOverCapacity   Cause = "forcages_au_dela_des_cadeaux"
	CauseNoRecipient          Cause = "personne_sans_destinataire"
	CauseNoGiver              Cause = "personne_sans_donneur"
	CauseHouseholdTooLarge    Cause = "foyer_trop_grand"
	CauseHistory              Cause = "historique"
	CauseAntiSwap             Cause = "anti_echange"
	CauseExclusions           Cause = "exclusions"
	CauseHouseholds           Cause = "foyers"
	CauseForced               Cause = "forcages"
	CauseTooConstrained       Cause = "regles_combinees"
	CauseSearchLimit          Cause = "recherche_trop_longue"
)

// Infeasible est le refus d'un tirage. Message et Suggestions sont en
// français et ne nomment jamais une personne ni une paire : seuls des
// comptes y figurent.
type Infeasible struct {
	Cause       Cause
	Message     string
	Missing     int      // cadeaux manquants (capacité insuffisante)
	Suggestions []string // propositions de correction
}

// Error renvoie le message complet, propositions comprises.
func (e *Infeasible) Error() string {
	if len(e.Suggestions) == 0 {
		return e.Message
	}
	return e.Message + " Propositions : " + strings.Join(e.Suggestions, " ")
}

var causeMessages = map[Cause]string{
	CauseInvalidInput:         "Le tirage ne peut pas être lancé : la liste des participants ou des forçages est incohérente.",
	CauseTooFewGivers:         "Le tirage est impossible : il faut au moins deux personnes qui offrent un cadeau, car personne ne peut s'offrir son propre cadeau.",
	CauseCapNeedsConfirmation: "Un plafond de plus de 2 cadeaux offerts par personne doit être confirmé explicitement avant le tirage.",
	CauseForcedReceptionOnly:  "Le tirage est impossible : un forçage désigne comme donneur une personne en « réception seule », qui n'offre aucun cadeau. Retirez ce forçage ou changez le rôle de cette personne.",
	CauseForcedSelf:           "Le tirage est impossible : un forçage désigne une personne comme son propre destinataire.",
	CauseForcedTwice:          "Le tirage est impossible : deux forçages désignent le même destinataire, alors que chacun reçoit exactement un cadeau.",
	CauseForcedConflict:       "Le tirage est impossible : un forçage contredit une exclusion, un foyer ou l'historique des années précédentes.",
	CauseForcedSwap:           "Le tirage est impossible : deux forçages forment un échange réciproque, interdit par l'option anti-échange.",
	CauseForcedOverCapacity:   "Le tirage est impossible : une personne a plus de forçages que de cadeaux à offrir (un forçage compte comme un des cadeaux de son donneur).",
	CauseNoRecipient:          "Le tirage est impossible : une personne n'a aucun destinataire possible avec les exclusions, les foyers et l'historique actuels.",
	CauseNoGiver:              "Le tirage est impossible : une personne ne peut recevoir de cadeau de personne avec les exclusions, les foyers et l'historique actuels.",
	CauseHouseholdTooLarge:    "Le tirage est impossible : un foyer est trop grand. Ses membres ne s'offrent pas de cadeaux entre eux et il n'y a pas assez de personnes en dehors du foyer.",
	CauseHistory:              "Le tirage est impossible : les exclusions des années précédentes ne laissent aucun tirage possible. Réduisez le nombre d'années prises en compte ou désactivez l'exclusion de l'année précédente.",
	CauseAntiSwap:             "Le tirage est impossible avec l'option anti-échange : désactivez-la ou assouplissez d'autres règles.",
	CauseExclusions:           "Le tirage est impossible : les exclusions ne laissent aucun tirage possible. Retirez-en quelques-unes.",
	CauseHouseholds:           "Le tirage est impossible : les foyers ne laissent aucun tirage possible avec les autres règles.",
	CauseForced:               "Le tirage est impossible : les forçages sont contradictoires avec les autres règles. Retirez-en quelques-uns.",
	CauseTooConstrained:       "Le tirage est impossible : ensemble, les exclusions, les foyers, les forçages, l'historique et l'option anti-échange ne laissent aucun tirage possible. Assouplissez quelques règles.",
	CauseSearchLimit:          "Aucun tirage n'a été trouvé dans la limite de recherche : les règles sont trop contraignantes. Assouplissez l'option anti-échange ou quelques exclusions.",
}

// previousYearOnly est le message d'historique quand seule l'année
// précédente est exclue.
const previousYearOnly = "Le tirage est impossible : exclure les paires de l'année précédente ne laisse aucun tirage possible. Désactivez cette option ou assouplissez d'autres règles."

func newInfeasible(c Cause) *Infeasible {
	return &Infeasible{Cause: c, Message: causeMessages[c]}
}

func giftCount(n int) string {
	if n > 1 {
		return fmt.Sprintf("%d cadeaux", n)
	}
	return fmt.Sprintf("%d cadeau", n)
}

func capacityMessage(c Cause, missing int) string {
	reason := "le plafond est trop bas"
	if c == CauseTooManyOneGift {
		reason = "trop de personnes ne peuvent pas ou ne veulent pas offrir plus d'un cadeau"
	}
	return fmt.Sprintf("Le tirage est impossible : %s ; il manque %s.", reason, giftCount(missing))
}

func capacitySuggestions() []string {
	return []string{
		"Demander à des personnes de passer en « J'offre plus ».",
		"Demander à des personnes de passer en « normal ».",
		"Relever le plafond de cadeaux par personne (au-delà de 2, une confirmation explicite est demandée).",
	}
}

// diagnose cherche la règle dont le seul assouplissement rend le tirage
// possible, dans l'ordre : historique, anti-échange, forçages, exclusions,
// foyers (des plus optionnelles aux plus structurelles) ; à défaut, c'est
// leur combinaison qui bloque. Renvoie toujours une
// erreur non nulle.
func (p *problem) diagnose(budget int) error {
	type relaxation struct {
		mask  int
		cause Cause
		apply bool
	}
	hasHistory := p.anyPair(p.forbidden) || (p.excludePrevious && p.anyPair(p.previous))
	hasHouseholds := false
	for _, h := range p.house {
		hasHouseholds = hasHouseholds || h != ""
	}
	tries := []relaxation{
		{relaxHistory, CauseHistory, hasHistory},
		{relaxAntiSwap, CauseAntiSwap, p.antiSwap},
		{relaxForced, CauseForced, len(p.forced) > 0},
		{relaxExclusions, CauseExclusions, p.anyPair(p.exclusions)},
		{relaxHouseholds, CauseHouseholds, hasHouseholds},
	}
	sawLimit := false
	for _, t := range tries {
		if !t.apply {
			continue
		}
		status, err := p.solvable(p.constraints(t.mask), budget)
		if err != nil {
			return err
		}
		switch status {
		case searchFound:
			if t.cause == CauseHistory {
				return p.historyInfeasible()
			}
			return newInfeasible(t.cause)
		case searchBudgetExceeded:
			sawLimit = true
		}
	}
	if sawLimit {
		return newInfeasible(CauseSearchLimit)
	}
	return newInfeasible(CauseTooConstrained)
}

func (p *problem) anyPair(m [][]bool) bool {
	for g := range m {
		for r := range m[g] {
			if m[g][r] && g != r {
				return true
			}
		}
	}
	return false
}

// historyInfeasible explique un refus dû à l'historique, en distinguant le
// cas où seule l'année précédente est exclue.
func (p *problem) historyInfeasible() *Infeasible {
	inf := newInfeasible(CauseHistory)
	if !p.anyPair(p.forbidden) {
		inf.Message = previousYearOnly
	}
	return inf
}
