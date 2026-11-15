package adjust

import "sort"

// impact décrit l'effet d'un ajustement, calculé sur le serveur. Les
// identifiants ne sortent jamais vers l'administrateur : seuls les comptes
// passent dans l'aperçu.
type impact struct {
	// affected sont les tireurs déjà dans le tirage dont les cibles changent
	// (cible gagnée ou perdue), triés. La personne ajoutée n'en fait pas
	// partie : elle n'avait pas de cible.
	affected []int64
	// notify sont les personnes à prévenir : les personnes touchées et la
	// personne ajoutée si elle a une cible, triées.
	notify []int64
	// revealed sont, parmi eux, ceux qui avaient déjà découvert une cible
	// qu'ils perdent.
	revealed map[int64]bool
	// gained sont ceux qui reçoivent au moins une nouvelle cible à découvrir.
	gained map[int64]bool
	// lost sont les paires actuelles abandonnées dont le tireur reste dans
	// le tirage (lettre de l'ancienne cible à archiver).
	lost []Pair
	// changed est le nombre de paires nouvelles.
	changed int
}

// measure compare le tirage actuel au tirage réparé. removed est la
// personne qui quitte le tirage (0 sans retrait) : elle n'est jamais comptée
// ni prévenue. added est la personne qui entre (0 sans ajout) : elle est
// prévenue de sa cible sans être comptée parmi les personnes touchées.
func measure(current []CurrentPair, next []Pair, removed, added int64) impact {
	cur := map[Pair]bool{}
	for _, c := range current {
		cur[Pair{c.Giver, c.Receiver}] = c.Discovered
	}
	nxt := map[Pair]bool{}
	for _, p := range next {
		nxt[p] = true
	}
	im := impact{revealed: map[int64]bool{}, gained: map[int64]bool{}}
	touched := map[int64]bool{}
	for _, c := range current {
		p := Pair{c.Giver, c.Receiver}
		if nxt[p] || c.Giver == removed {
			continue
		}
		touched[c.Giver] = true
		im.lost = append(im.lost, p)
		if c.Discovered {
			im.revealed[c.Giver] = true
		}
	}
	for _, p := range next {
		if _, ok := cur[p]; ok {
			continue
		}
		im.changed++
		touched[p.Giver] = true
		im.gained[p.Giver] = true
	}
	for id := range touched {
		if id != added {
			im.affected = append(im.affected, id)
		}
		im.notify = append(im.notify, id)
	}
	sort.Slice(im.affected, func(i, j int) bool { return im.affected[i] < im.affected[j] })
	sort.Slice(im.notify, func(i, j int) bool { return im.notify[i] < im.notify[j] })
	sortPairs(im.lost)
	return im
}
