package drawengine

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// tb est la partie commune de *testing.T et *rapid.T.
type tb interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

var _ tb = (*testing.T)(nil)

// Oracle indépendant du moteur : il relit l'entrée sans passer par problem.

type oracle struct {
	in      Input
	ids     []int64
	idx     map[int64]int
	role    []Role
	cap     int
	ok      bool // l'instance est recevable (plafond confirmé, capacité suffisante, au moins deux tireurs)
	degLo   []int
	degHi   []int
	tierSum map[Role]int
	tierOf  map[Role][]int
}

func roleOf(p Participant) Role {
	if p.Role == "" {
		return RoleNormal
	}
	return p.Role
}

func newOracle(in Input) *oracle {
	o := &oracle{in: in, idx: map[int64]int{}, tierSum: map[Role]int{}, tierOf: map[Role][]int{}}
	o.cap = in.Cap
	if o.cap == 0 {
		o.cap = 2
	}
	for i, p := range in.Participants {
		o.ids = append(o.ids, p.ID)
		o.idx[p.ID] = i
		o.role = append(o.role, roleOf(p))
		o.tierOf[roleOf(p)] = append(o.tierOf[roleOf(p)], i)
	}
	n := len(o.ids)
	r := len(o.tierOf[RoleReceptionOnly])
	v := len(o.tierOf[RoleGivesMore])
	m := len(o.tierOf[RoleNormal])
	givers := n - r
	extra := o.cap - 1
	eop := r
	if v*extra < eop {
		eop = v * extra
	}
	en := r - eop
	o.ok = givers >= 2 && (o.cap <= 2 || in.CapConfirmed) && en <= m*extra
	o.degLo = make([]int, n)
	o.degHi = make([]int, n)
	set := func(role Role, e int) {
		s := len(o.tierOf[role])
		if s == 0 {
			return
		}
		o.tierSum[role] = s + e
		for _, i := range o.tierOf[role] {
			o.degLo[i] = 1 + e/s
			o.degHi[i] = 1 + (e+s-1)/s
		}
	}
	set(RoleGivesMore, eop)
	set(RoleNormal, en)
	set(RoleOneGift, 0)
	return o
}

func (o *oracle) allowed(g, r int) bool {
	if g == r || o.role[g] == RoleReceptionOnly {
		return false
	}
	pg, pr := o.in.Participants[g], o.in.Participants[r]
	if pg.Household != "" && pg.Household == pr.Household {
		return false
	}
	has := func(ps []Pair) bool {
		for _, p := range ps {
			if p.Giver == pg.ID && p.Receiver == pr.ID {
				return true
			}
		}
		return false
	}
	if has(o.in.Exclusions) || has(o.in.Forbidden) {
		return false
	}
	if o.in.ExcludePreviousYear && has(o.in.PreviousYear) {
		return false
	}
	return true
}

// enumerate renvoie toutes les affectations valides (assign[r] = tireur),
// jusqu'à limit (0 = sans limite).
func (o *oracle) enumerate(limit int) [][]int {
	if !o.ok {
		return nil
	}
	n := len(o.ids)
	forced := make([]int, n)
	for i := range forced {
		forced[i] = -1
	}
	for _, f := range o.in.Forced {
		g, r := o.idx[f.Giver], o.idx[f.Receiver]
		if forced[r] >= 0 && forced[r] != g {
			return nil
		}
		forced[r] = g
	}
	var out [][]int
	assign := make([]int, n)
	deg := make([]int, n)
	var rec func(r int)
	rec = func(r int) {
		if limit > 0 && len(out) >= limit {
			return
		}
		if r == n {
			if o.leafOK(assign, deg) {
				out = append(out, append([]int(nil), assign...))
			}
			return
		}
		for g := 0; g < n; g++ {
			if forced[r] >= 0 && forced[r] != g {
				continue
			}
			if !o.allowed(g, r) || deg[g] >= o.degHi[g] {
				continue
			}
			assign[r] = g
			deg[g]++
			rec(r + 1)
			deg[g]--
		}
	}
	rec(0)
	return out
}

func (o *oracle) leafOK(assign, deg []int) bool {
	sums := map[Role]int{}
	for i := range assign {
		if o.role[i] == RoleReceptionOnly {
			if deg[i] != 0 {
				return false
			}
			continue
		}
		if deg[i] < o.degLo[i] || deg[i] > o.degHi[i] || deg[i] > o.cap {
			return false
		}
		sums[o.role[i]] += deg[i]
	}
	for role, s := range o.tierSum {
		if sums[role] != s {
			return false
		}
	}
	if o.in.AntiSwap {
		for r, g := range assign {
			if assign[g] == r {
				return false
			}
		}
	}
	return true
}

// toAssign convertit un résultat en assign[r] = tireur, en vérifiant que chaque
// participant reçoit exactement un cadeau.
func (o *oracle) toAssign(res *Result) ([]int, error) {
	n := len(o.ids)
	if len(res.Pairs) != n {
		return nil, fmt.Errorf("%d paires pour %d participants", len(res.Pairs), n)
	}
	assign := make([]int, n)
	for i := range assign {
		assign[i] = -1
	}
	for _, p := range res.Pairs {
		g, okG := o.idx[p.Giver]
		r, okR := o.idx[p.Receiver]
		if !okG || !okR {
			return nil, fmt.Errorf("paire inconnue %v", p)
		}
		if assign[r] >= 0 {
			return nil, fmt.Errorf("%d reçoit deux cadeaux", p.Receiver)
		}
		assign[r] = g
	}
	return assign, nil
}

// verify contrôle toutes les règles d'un résultat.
func (o *oracle) verify(res *Result) error {
	assign, err := o.toAssign(res)
	if err != nil {
		return err
	}
	deg := make([]int, len(assign))
	for r, g := range assign {
		if !o.allowed(g, r) {
			return fmt.Errorf("paire interdite %d→%d", o.ids[g], o.ids[r])
		}
		deg[g]++
	}
	for _, f := range o.in.Forced {
		if assign[o.idx[f.Receiver]] != o.idx[f.Giver] {
			return fmt.Errorf("forçage %v non respecté", f)
		}
	}
	if !o.leafOK(assign, deg) {
		return fmt.Errorf("quotas ou anti-échange non respectés : degrés %v", deg)
	}
	return nil
}

func (o *oracle) degrees(res *Result) map[int64]int {
	d := map[int64]int{}
	for _, id := range o.ids {
		d[id] = 0
	}
	for _, p := range res.Pairs {
		d[p.Giver]++
	}
	return d
}

func assignKey(a []int) string {
	parts := make([]string, len(a))
	for i, g := range a {
		parts[i] = strconv.Itoa(g)
	}
	return strings.Join(parts, ",")
}

// people construit des participants d'identifiants 1001, 1002…
func people(roles ...Role) []Participant {
	ps := make([]Participant, len(roles))
	for i, r := range roles {
		ps[i] = Participant{ID: int64(1001 + i), Role: r}
	}
	return ps
}

func normals(n int) []Participant {
	roles := make([]Role, n)
	for i := range roles {
		roles[i] = RoleNormal
	}
	return people(roles...)
}

func asInfeasible(t tb, err error) *Infeasible {
	t.Helper()
	var inf *Infeasible
	if !errors.As(err, &inf) {
		t.Fatalf("attendu un refus *Infeasible, obtenu %v", err)
	}
	if strings.TrimSpace(inf.Message) == "" {
		t.Fatal("refus sans explication")
	}
	return inf
}

// assertNoIdentity vérifie qu'aucun identifiant de participant n'apparaît
// dans le texte du refus.
func assertNoIdentity(t tb, in Input, err error) {
	t.Helper()
	text := err.Error()
	for _, p := range in.Participants {
		if strings.Contains(text, strconv.FormatInt(p.ID, 10)) {
			t.Fatalf("le refus cite un participant : %q", text)
		}
		if p.Household != "" && strings.Contains(text, p.Household) {
			t.Fatalf("le refus cite un foyer : %q", text)
		}
	}
}

func mustDraw(t tb, in Input) *Result {
	t.Helper()
	res, err := Draw(in)
	if err != nil {
		t.Fatalf("tirage refusé : %v", err)
	}
	if err := newOracle(in).verify(res); err != nil {
		t.Fatalf("tirage invalide : %v", err)
	}
	return res
}

// chiSquareCritical approche le quantile 1-α de la loi du χ² (Wilson-Hilferty),
// z étant le quantile normal correspondant.
func chiSquareCritical(df int, z float64) float64 {
	k := float64(df)
	a := 2 / (9 * k)
	return k * math.Pow(1-a+z*math.Sqrt(a), 3)
}

// zAlpha001 est le quantile normal à 0,999 (α = 0,001).
const zAlpha001 = 3.0902

// chiSquareUniform tire draws affectations, vérifie que chacune est une des
// affectations valides énumérées et renvoie la statistique et le seuil.
func chiSquareUniform(t *testing.T, in Input, draws int) (stat, critical float64, cells int) {
	t.Helper()
	o := newOracle(in)
	valid := o.enumerate(0)
	if len(valid) < 2 {
		t.Fatalf("instance trop petite : %d affectations valides", len(valid))
	}
	counts := map[string]int{}
	for _, a := range valid {
		counts[assignKey(a)] = 0
	}
	for i := 0; i < draws; i++ {
		res, err := Draw(in)
		if err != nil {
			t.Fatal(err)
		}
		a, err := o.toAssign(res)
		if err != nil {
			t.Fatal(err)
		}
		k := assignKey(a)
		if _, ok := counts[k]; !ok {
			t.Fatalf("affectation hors de l'ensemble valide : %v", res.Pairs)
		}
		counts[k]++
	}
	expected := float64(draws) / float64(len(valid))
	if expected < 5 {
		t.Fatalf("effectif attendu trop faible : %.1f", expected)
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := float64(counts[k]) - expected
		stat += d * d / expected
	}
	return stat, chiSquareCritical(len(valid)-1, zAlpha001), len(valid)
}
