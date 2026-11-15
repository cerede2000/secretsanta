package givernotify

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"secretsanta/internal/letter"
	"secretsanta/internal/mail/queue"
	"secretsanta/internal/shared/ports"
)

var errPanne = errors.New("panne")

type zzSuppress struct {
	err error
	yes bool
}

func (s zzSuppress) IsSuppressed(context.Context, int64, string) (bool, error) { return s.yes, s.err }
func (zzSuppress) Add(context.Context, int64, string, string) error            { return nil }
func (zzSuppress) Remove(context.Context, int64, string) error                 { return nil }

type zzQueue struct{}

func (zzQueue) EnqueueMail(context.Context, queue.Item) (int64, error) { return 0, errPanne }

type zzGate struct{ err error }

func (g zzGate) IsOpen(context.Context, int64) (bool, error) { return true, g.err }

type zzLetters struct{}

func (zzLetters) MarkNotified(context.Context, int64) error { return errPanne }

func zzEvent(gid, receiver, letterID int64) ports.Event {
	return ports.Event{GroupID: gid, Payload: map[string]string{
		letter.PayloadRecipientID: strconv.FormatInt(receiver, 10),
		letter.PayloadLetterID:    strconv.FormatInt(letterID, 10),
	}}
}

func TestZZHandleUpdatedFailures(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	_, ps := e.firstGiverWith(t, gid, 1)
	ev := zzEvent(gid, ps[0].Receiver, 1)

	if err := e.svc.HandleUpdated(ctx, ports.Event{GroupID: gid, Payload: map[string]string{letter.PayloadRecipientID: "x"}}); err == nil {
		t.Error("charge invalide acceptée")
	}
	// Destinataire sans tireur : rien à faire.
	if err := e.svc.HandleUpdated(ctx, zzEvent(gid, 99999, 1)); err != nil {
		t.Errorf("sans tireur : %v", err)
	}
	orig := e.svc.o
	restore := func() { e.svc.o = orig }

	e.svc.o.Suppress = zzSuppress{err: errPanne}
	if e.svc.HandleUpdated(ctx, ev) == nil {
		t.Error("panne de la liste de suppression ignorée")
	}
	restore()
	e.svc.o.Suppress = zzSuppress{yes: true}
	if err := e.svc.HandleUpdated(ctx, ev); err != nil {
		t.Errorf("adresse supprimée : %v", err)
	}
	restore()
	e.svc.o.Gate = zzGate{err: errPanne}
	if e.svc.HandleUpdated(ctx, ev) == nil {
		t.Error("panne de la porte ignorée")
	}
	restore()
	e.svc.o.Queue = zzQueue{}
	if e.svc.HandleUpdated(ctx, ev) == nil {
		t.Error("panne de la file ignorée")
	}
	restore()
	e.svc.o.Letters = zzLetters{}
	if e.svc.HandleUpdated(ctx, ev) == nil {
		t.Error("panne de MarkNotified ignorée")
	}
	restore()
}

func TestZZTickFailures(t *testing.T) {
	e := newEnv(t, "fr")
	gid, _ := e.singleGroup(t, "famille")
	_, ps := e.firstGiverWith(t, gid, 1)
	e.edit(t, gid, ps[0].Receiver, 1)
	e.clock.Advance(8 * day)
	orig := e.svc.o

	e.svc.o.Suppress = zzSuppress{err: errPanne}
	if _, err := e.svc.Tick(ctx); err == nil {
		t.Error("panne de suppression ignorée")
	}
	e.svc.o = orig
	e.svc.o.Suppress = zzSuppress{yes: true}
	if r, err := e.svc.Tick(ctx); err != nil || r.Queued != 0 {
		t.Errorf("adresses supprimées : %+v %v", r, err)
	}
	e.svc.o = orig
	e.svc.o.Queue = zzQueue{}
	if _, err := e.svc.Tick(ctx); err == nil {
		t.Error("panne de file ignorée")
	}
	e.svc.o = orig
	e.svc.o.Gate = zzGate{err: errPanne}
	if _, err := e.svc.Tick(ctx); err == nil {
		t.Error("panne de porte ignorée")
	}
	e.svc.o = orig
	_ = e.db.Close()
	if _, err := e.svc.Tick(ctx); err == nil {
		t.Error("base fermée acceptée")
	}
	if err := e.svc.HandleUpdated(ctx, zzEvent(gid, ps[0].Receiver, 1)); err == nil {
		t.Error("HandleUpdated sur base fermée accepté")
	}
}
