package drawengine

// Niveaux de tireurs, dans l'ordre de priorité des cadeaux supplémentaires.
const (
	tierGivesMore = iota
	tierNormal
	tierOneGift
	tierCount
	tierNone = -1 // réception seule : n'offre rien
)

// problem est l'instance indexée : les participants sont désignés par leur
// position 0..n-1.
type problem struct {
	n     int
	ids   []int64
	index map[int64]int
	house []string
	tier  []int // tierNone pour une réception seule

	cap       int
	tierSize  [tierCount]int
	tierTotal [tierCount]int // cadeaux offerts par le niveau
	tierLo    [tierCount]int // cadeaux offerts au minimum par un tireur du niveau
	tierHi    [tierCount]int // cadeaux offerts au maximum par un tireur du niveau
	givers    int
	reception int

	exclusions      [][]bool
	forbidden       [][]bool
	previous        [][]bool
	excludePrevious bool
	forced          [][2]int // {tireur, destinataire}, sans doublon
	antiSwap        bool
	capConfirmed    bool
}

// Assouplissements utilisés pour diagnostiquer une impossibilité.
const (
	relaxHistory = 1 << iota
	relaxAntiSwap
	relaxExclusions
	relaxHouseholds
	relaxForced
)

// cons est le jeu de contraintes effectif pour un assouplissement donné.
type cons struct {
	allowed  [][]bool // allowed[g][r] : g peut offrir à r
	forced   []int    // forced[r] = tireur imposé, ou -1
	antiSwap bool
}

func newProblem(in Input) (*problem, *Infeasible) {
	n := len(in.Participants)
	if n == 0 {
		return nil, newInfeasible(CauseInvalidInput)
	}
	if in.Cap < 0 {
		return nil, newInfeasible(CauseInvalidInput)
	}
	p := &problem{
		n:               n,
		ids:             make([]int64, n),
		index:           make(map[int64]int, n),
		house:           make([]string, n),
		tier:            make([]int, n),
		cap:             in.Cap,
		excludePrevious: in.ExcludePreviousYear,
		antiSwap:        in.AntiSwap,
		capConfirmed:    in.CapConfirmed,
	}
	if p.cap == 0 {
		p.cap = DefaultCap
	}
	for i, pa := range in.Participants {
		if _, dup := p.index[pa.ID]; dup {
			return nil, newInfeasible(CauseInvalidInput)
		}
		p.index[pa.ID] = i
		p.ids[i] = pa.ID
		p.house[i] = pa.Household
		switch pa.Role {
		case RoleNormal, "":
			p.tier[i] = tierNormal
		case RoleGivesMore:
			p.tier[i] = tierGivesMore
		case RoleOneGift:
			p.tier[i] = tierOneGift
		case RoleReceptionOnly:
			p.tier[i] = tierNone
		default:
			return nil, newInfeasible(CauseInvalidInput)
		}
		if p.tier[i] == tierNone {
			p.reception++
		} else {
			p.tierSize[p.tier[i]]++
			p.givers++
		}
	}
	p.exclusions = p.pairMatrix(in.Exclusions)
	p.forbidden = p.pairMatrix(in.Forbidden)
	p.previous = p.pairMatrix(in.PreviousYear)
	seen := map[[2]int]bool{}
	for _, f := range in.Forced {
		g, okG := p.index[f.Giver]
		r, okR := p.index[f.Receiver]
		if !okG || !okR {
			return nil, newInfeasible(CauseInvalidInput)
		}
		k := [2]int{g, r}
		if !seen[k] {
			seen[k] = true
			p.forced = append(p.forced, k)
		}
	}
	return p, nil
}

// pairMatrix ignore les paires qui citent une personne absente : elles ne
// contraignent rien.
func (p *problem) pairMatrix(pairs []Pair) [][]bool {
	m := make([][]bool, p.n)
	for i := range m {
		m[i] = make([]bool, p.n)
	}
	for _, pr := range pairs {
		g, okG := p.index[pr.Giver]
		r, okR := p.index[pr.Receiver]
		if okG && okR {
			m[g][r] = true
		}
	}
	return m
}

// distribute répartit les R cadeaux supplémentaires : d'abord les offre_plus,
// puis les normal, jamais les un_seul ; renvoie le nombre de cadeaux
// manquants (0 si la capacité suffit).
func (p *problem) distribute() int {
	extra := p.cap - 1
	if extra < 0 {
		extra = 0
	}
	r := p.reception
	var extras [tierCount]int
	extras[tierGivesMore] = min(r, p.tierSize[tierGivesMore]*extra)
	extras[tierNormal] = min(r-extras[tierGivesMore], p.tierSize[tierNormal]*extra)
	missing := r - extras[tierGivesMore] - extras[tierNormal]
	for t := 0; t < tierCount; t++ {
		s := p.tierSize[t]
		p.tierTotal[t] = s + extras[t]
		if s == 0 {
			continue
		}
		p.tierLo[t] = 1 + extras[t]/s
		p.tierHi[t] = 1 + (extras[t]+s-1)/s
	}
	return missing
}

// capacityRaw est la capacité de cadeaux supplémentaires du groupe.
func (p *problem) capacityRaw() int {
	extra := max(p.cap-1, 0)
	return (p.tierSize[tierGivesMore] + p.tierSize[tierNormal]) * extra
}

func (p *problem) constraints(relax int) cons {
	c := cons{
		allowed:  make([][]bool, p.n),
		forced:   make([]int, p.n),
		antiSwap: p.antiSwap && relax&relaxAntiSwap == 0,
	}
	for g := 0; g < p.n; g++ {
		c.allowed[g] = make([]bool, p.n)
		if p.tier[g] == tierNone {
			continue
		}
		for r := 0; r < p.n; r++ {
			c.allowed[g][r] = p.pairAllowed(g, r, relax)
		}
	}
	for r := range c.forced {
		c.forced[r] = -1
	}
	if relax&relaxForced == 0 {
		for _, f := range p.forced {
			c.forced[f[1]] = f[0]
		}
	}
	return c
}

func (p *problem) pairAllowed(g, r, relax int) bool {
	if g == r || p.tier[g] == tierNone {
		return false
	}
	if relax&relaxHouseholds == 0 && p.house[g] != "" && p.house[g] == p.house[r] {
		return false
	}
	if relax&relaxExclusions == 0 && p.exclusions[g][r] {
		return false
	}
	if relax&relaxHistory == 0 {
		if p.forbidden[g][r] || (p.excludePrevious && p.previous[g][r]) {
			return false
		}
	}
	return true
}

// precheck repère les causes d'impossibilité qui s'expliquent seules, dans
// l'ordre du plus fondamental au plus particulier.
func (p *problem) precheck() *Infeasible {
	if p.givers < 2 {
		return newInfeasible(CauseTooFewGivers)
	}
	if p.cap > MaxUnconfirmedCap && !p.capConfirmed {
		return newInfeasible(CauseCapNeedsConfirmation)
	}
	if missing := p.distribute(); missing > 0 {
		return p.capacityInfeasible(missing)
	}
	if inf := p.precheckForced(); inf != nil {
		return inf
	}
	if p.householdTooLarge() {
		return newInfeasible(CauseHouseholdTooLarge)
	}
	full := p.constraints(0)
	if cause := p.isolated(full); cause != "" {
		// Si l'historique seul isole cette personne, c'est lui la cause.
		if p.isolated(p.constraints(relaxHistory)) == "" {
			return p.historyInfeasible()
		}
		return newInfeasible(cause)
	}
	return nil
}

// isolated renvoie CauseNoRecipient si un tireur n'a aucun destinataire
// possible, CauseNoGiver si une personne n'a aucun donneur possible, "" sinon.
func (p *problem) isolated(c cons) Cause {
	for g := 0; g < p.n; g++ {
		if p.tier[g] == tierNone {
			continue
		}
		found := false
		for r := 0; r < p.n && !found; r++ {
			found = c.allowed[g][r]
		}
		if !found {
			return CauseNoRecipient
		}
	}
	for r := 0; r < p.n; r++ {
		found := false
		for g := 0; g < p.n && !found; g++ {
			found = c.allowed[g][r]
		}
		if !found {
			return CauseNoGiver
		}
	}
	return ""
}

func (p *problem) capacityInfeasible(missing int) *Infeasible {
	cause := CauseCapTooLow
	if p.cap >= 2 && (p.tierSize[tierOneGift] > 0 || p.capacityRaw() == 0) {
		cause = CauseTooManyOneGift
	}
	inf := newInfeasible(cause)
	inf.Missing = missing
	inf.Message = capacityMessage(cause, missing)
	inf.Suggestions = capacitySuggestions()
	return inf
}

func (p *problem) precheckForced() *Infeasible {
	receiverOf := make([]int, p.n)
	perGiver := make([]int, p.n)
	for i := range receiverOf {
		receiverOf[i] = -1
	}
	isForced := map[[2]int]bool{}
	for _, f := range p.forced {
		isForced[f] = true
	}
	for _, f := range p.forced {
		g, r := f[0], f[1]
		if p.tier[g] == tierNone {
			return newInfeasible(CauseForcedReceptionOnly)
		}
		if g == r {
			return newInfeasible(CauseForcedSelf)
		}
		if receiverOf[r] != -1 && receiverOf[r] != g {
			return newInfeasible(CauseForcedTwice)
		}
		receiverOf[r] = g
		if !p.pairAllowed(g, r, 0) {
			return newInfeasible(CauseForcedConflict)
		}
		if p.antiSwap && isForced[[2]int{r, g}] {
			return newInfeasible(CauseForcedSwap)
		}
		perGiver[g]++
		if perGiver[g] > p.tierHi[p.tier[g]] {
			return newInfeasible(CauseForcedOverCapacity)
		}
	}
	return nil
}

// householdTooLarge applique les conditions de Hall propres à un foyer : ses
// membres reçoivent tous de l'extérieur, et ses tireurs offrent tous à
// l'extérieur.
func (p *problem) householdTooLarge() bool {
	members := map[string][]int{}
	for i, h := range p.house {
		if h != "" {
			members[h] = append(members[h], i)
		}
	}
	for _, ms := range members {
		var in [tierCount]int
		for _, i := range ms {
			if p.tier[i] != tierNone {
				in[p.tier[i]]++
			}
		}
		supplyOut, demandIn := 0, 0
		for t := 0; t < tierCount; t++ {
			out := p.tierSize[t] - in[t]
			supplyOut += min(p.tierTotal[t], out*p.tierHi[t])
			demandIn += max(in[t]*p.tierLo[t], p.tierTotal[t]-out*p.tierHi[t])
		}
		if len(ms) > supplyOut || demandIn > p.n-len(ms) {
			return true
		}
	}
	return false
}

func (p *problem) initialAssign(c cons) []int {
	a := make([]int, p.n)
	copy(a, c.forced)
	return a
}

// valid contrôle une affectation complète (assign[r] = tireur de r) : les
// quotas de niveau sont garantis par construction du tirage.
func (p *problem) valid(c cons, assign []int) bool {
	for r, g := range assign {
		if g < 0 || !c.allowed[g][r] {
			return false
		}
		if c.forced[r] >= 0 && c.forced[r] != g {
			return false
		}
		if c.antiSwap && assign[g] == r {
			return false
		}
	}
	return true
}
