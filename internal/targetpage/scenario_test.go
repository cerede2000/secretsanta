package targetpage

import (
	"strconv"
	"testing"
	"time"

	"secretsanta/internal/participants"
)

// scene est un groupe tiré avec trois rôles de chaque sorte.
type scene struct {
	gid       int64
	slug      string
	reception []int64
	more      []int64
	normal    []int64
}

// newScene prépare un groupe (revealAt nil : cibles immédiates après tirage),
// ajoute deux personnes de chaque rôle, et tire si draw est vrai.
func newScene(t *testing.T, e *env, slug string, revealAt *time.Time, drawIt bool) *scene {
	t.Helper()
	s := &scene{gid: e.group(t, slug, revealAt), slug: slug}
	for _, n := range []string{"Hortensia", "Hermine"} {
		s.reception = append(s.reception, e.add(t, s.gid, n, participants.RoleReceptionSeule))
	}
	for _, n := range []string{"Gaspard", "Gontran"} {
		s.more = append(s.more, e.add(t, s.gid, n, participants.RoleOffrePlus))
	}
	for _, n := range []string{"Xavier", "Xenia"} {
		s.normal = append(s.normal, e.add(t, s.gid, n, participants.RoleNormal))
	}
	if drawIt {
		e.mustDraw(t, s.gid)
	}
	return s
}

func (e *env) role(t *testing.T, gid, pid int64) string {
	t.Helper()
	var r string
	if err := e.db.QueryRow(`SELECT draw_role FROM participants WHERE group_id = ? AND id = ?`, gid, pid).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

// discoverAll découvre toutes les cibles de la personne ; renvoie les paires.
func (e *env) discover(t *testing.T, gid, pid int64, only int) {
	t.Helper()
	mine, err := e.draw.MyTargets(ctx, participants.ParticipantActor(gid, pid), gid)
	if err != nil {
		t.Fatal(err)
	}
	for i, tg := range mine.Targets {
		if only >= 0 && i != only {
			continue
		}
		if _, err := e.draw.MarkDiscovered(ctx, participants.ParticipantActor(gid, pid), gid, tg.PairID); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *env) firstName(t *testing.T, id int64) string {
	t.Helper()
	var n string
	if err := e.db.QueryRow(`SELECT first_name FROM participants WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
