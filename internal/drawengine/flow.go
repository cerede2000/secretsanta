package drawengine

// Flot maximum de Dinic. Sur un graphe biparti à capacités 1 il se réduit à
// Hopcroft-Karp ; ici les tireurs portent des capacités (quotas de cadeaux)
// avec bornes inférieures, traitées par la réduction classique en circulation.

const flowInf = 1 << 30

type dinic struct {
	adj   [][]int
	to    []int
	cap   []int
	level []int
	iter  []int
}

func newDinic(n int) *dinic {
	return &dinic{adj: make([][]int, n), level: make([]int, n), iter: make([]int, n)}
}

func (d *dinic) addEdge(u, v, c int) {
	d.adj[u] = append(d.adj[u], len(d.to))
	d.to = append(d.to, v)
	d.cap = append(d.cap, c)
	d.adj[v] = append(d.adj[v], len(d.to))
	d.to = append(d.to, u)
	d.cap = append(d.cap, 0)
}

func (d *dinic) bfs(s, t int) bool {
	for i := range d.level {
		d.level[i] = -1
	}
	d.level[s] = 0
	queue := []int{s}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, e := range d.adj[u] {
			if d.cap[e] > 0 && d.level[d.to[e]] < 0 {
				d.level[d.to[e]] = d.level[u] + 1
				queue = append(queue, d.to[e])
			}
		}
	}
	return d.level[t] >= 0
}

func (d *dinic) dfs(u, t, f int) int {
	if u == t {
		return f
	}
	for ; d.iter[u] < len(d.adj[u]); d.iter[u]++ {
		e := d.adj[u][d.iter[u]]
		v := d.to[e]
		if d.cap[e] > 0 && d.level[v] == d.level[u]+1 {
			if got := d.dfs(v, t, min(f, d.cap[e])); got > 0 {
				d.cap[e] -= got
				d.cap[e^1] += got
				return got
			}
		}
	}
	return 0
}

func (d *dinic) maxFlow(s, t int) int {
	total := 0
	for d.bfs(s, t) {
		for i := range d.iter {
			d.iter[i] = 0
		}
		for f := d.dfs(s, t, flowInf); f > 0; f = d.dfs(s, t, flowInf) {
			total += f
		}
	}
	return total
}

// boundedNetwork est un réseau dont les arcs portent [lo, hi].
type boundedNetwork struct {
	n      int
	d      *dinic
	excess []int
}

func newBoundedNetwork(n int) *boundedNetwork {
	// Deux sommets de plus : super-source et super-puits.
	return &boundedNetwork{n: n, d: newDinic(n + 2), excess: make([]int, n)}
}

func (b *boundedNetwork) add(u, v, lo, hi int) bool {
	if lo > hi {
		return false
	}
	if hi > lo {
		b.d.addEdge(u, v, hi-lo)
	}
	b.excess[v] += lo
	b.excess[u] -= lo
	return true
}

// feasible dit si une circulation respecte toutes les bornes, avec un arc de
// retour t→s de capacité infinie.
func (b *boundedNetwork) feasible(s, t int) bool {
	b.d.addEdge(t, s, flowInf)
	ss, tt := b.n, b.n+1
	need := 0
	for v, e := range b.excess {
		switch {
		case e > 0:
			b.d.addEdge(ss, v, e)
			need += e
		case e < 0:
			b.d.addEdge(v, tt, -e)
		}
	}
	return b.d.maxFlow(ss, tt) == need
}

// flowFeasible dit si les affectations partielles assign (assign[r] = tireur
// imposé ou -1) se complètent en une affectation respectant les quotas de
// niveau, les paires permises et, pour les paires déjà fixées, l'anti-échange.
// Sans anti-échange, la réponse est exacte ; avec, c'est une condition
// nécessaire (deux paires encore libres peuvent former un échange).
func (p *problem) flowFeasible(c cons, assign []int) bool {
	const (
		src  = 0
		sink = 1
	)
	tierNode := func(t int) int { return 2 + t }
	giverNode := func(i int) int { return 2 + tierCount + i }
	recvNode := func(i int) int { return 2 + tierCount + p.n + i }
	net := newBoundedNetwork(2 + tierCount + 2*p.n)
	for t := 0; t < tierCount; t++ {
		if p.tierSize[t] > 0 {
			net.add(src, tierNode(t), p.tierTotal[t], p.tierTotal[t])
		}
	}
	for g := 0; g < p.n; g++ {
		if t := p.tier[g]; t != tierNone {
			net.add(tierNode(t), giverNode(g), p.tierLo[t], p.tierHi[t])
		}
	}
	for r := 0; r < p.n; r++ {
		net.add(recvNode(r), sink, 1, 1)
		if g := assign[r]; g >= 0 {
			if !c.allowed[g][r] {
				return false
			}
			if c.antiSwap && assign[g] == r {
				return false
			}
			net.add(giverNode(g), recvNode(r), 1, 1)
			continue
		}
		for g := 0; g < p.n; g++ {
			if !c.allowed[g][r] {
				continue
			}
			// Anti-échange : r offre déjà à g, donc g ne peut pas offrir à r.
			if c.antiSwap && assign[g] == r {
				continue
			}
			net.add(giverNode(g), recvNode(r), 0, 1)
		}
	}
	return net.feasible(src, sink)
}
