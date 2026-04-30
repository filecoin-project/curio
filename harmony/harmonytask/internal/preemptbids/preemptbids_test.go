package preemptbids

import (
	"testing"
	"time"
)

func TestRegisterDeliver(t *testing.T) {
	r := New()
	ch, cancel := r.Register(1, 4)
	defer cancel()

	r.Deliver(1, Response{PeerID: 10, Cost: time.Second})
	select {
	case got := <-ch:
		if got.PeerID != 10 || got.Cost != time.Second {
			t.Fatalf("unexpected response: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery")
	}
}

func TestDeliverBeforeRegisterIsBuffered(t *testing.T) {
	r := New()
	r.Deliver(2, Response{PeerID: 1, Cost: 1})
	r.Deliver(2, Response{PeerID: 2, Cost: 2})
	ch, cancel := r.Register(2, 0)
	defer cancel()

	got := []Response{<-ch, <-ch}
	if got[0].PeerID != 1 || got[1].PeerID != 2 {
		t.Fatalf("expected buffered responses in order, got %+v", got)
	}
}

func TestCancelDropsBuffered(t *testing.T) {
	r := New()
	r.Deliver(3, Response{PeerID: 7})
	_, cancel := r.Register(3, 4)
	cancel()
	r.Deliver(3, Response{PeerID: 8})

	if len(r.pending[3]) != 1 {
		t.Fatalf("expected 1 pending after cancel+deliver, got %d", len(r.pending[3]))
	}
}

func TestDeliverFullChannelDrops(t *testing.T) {
	r := New()
	ch, cancel := r.Register(4, 1)
	defer cancel()
	r.Deliver(4, Response{PeerID: 1})
	r.Deliver(4, Response{PeerID: 2}) // should drop
	<-ch
	select {
	case got := <-ch:
		t.Fatalf("expected drop, got %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
}
