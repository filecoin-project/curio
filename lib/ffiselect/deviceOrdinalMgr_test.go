package ffiselect

import (
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/resources"
)

func TestNoGPUs(t *testing.T) {
	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{}, nil
	})

	// Test get and release
	ord := d.Get()
	d.Release(ord)

	acquireChan := make(chan int)
	d.acquireChan <- acquireChan
	select {
	case ord = <-acquireChan:
		if ord != 0 {
			t.Fatal("should have gotten GPU 0")
		}
	case <-time.After(time.Millisecond * 100):
		t.Fatal("timeout")
	}

	d.acquireChan <- acquireChan
	select {
	case ord = <-acquireChan:
		t.Fatal("should not have gotten a GPU")
	case <-time.After(time.Millisecond * 100):
		// preferred outcome: no additional GPU
	}
	d.Release(ord)

	select {
	case ord = <-acquireChan:
		if ord != 0 {
			t.Fatal("should have gotten GPU 0")
		}
	case <-time.After(time.Millisecond * 100):
		t.Fatal("timeout")
	}
	d.Release(ord)
}

func TestOverprovisionFactor(t *testing.T) {
	old := resources.GpuOverprovisionFactor
	resources.GpuOverprovisionFactor = 2
	defer func() {
		resources.GpuOverprovisionFactor = old
	}()

	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{"0", "1", "2"}, nil
	})

	acquireChan := make(chan int)

	expect := []int{0, 1, 2, 0, 1, 2}
	for i, ord := range expect {
		d.acquireChan <- acquireChan
		select {
		case ord = <-acquireChan:
			if ord != expect[i] {
				t.Fatal("should have gotten GPU", expect[i])
			}
		case <-time.After(time.Millisecond * 100):
			t.Fatal("timeout")
		}
	}
	d.Release(1)
	d.Release(2)
	d.Release(1)

	// now 1 is the least used, so it should get it
	d.acquireChan <- acquireChan
	select {
	case ord := <-acquireChan:
		if ord != 1 {
			t.Fatal("should have gotten GPU 1")
		}
	case <-time.After(time.Millisecond * 100):
		t.Fatal("timeout")
	}
}

func TestWaitList(t *testing.T) {
	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{"a", "b", "c"}, nil
	})

	acquireChan := make(chan int)
	expect := []int{0, 1, 2}
	for i, ord := range expect {
		d.acquireChan <- acquireChan
		select {
		case ord = <-acquireChan:
			if ord != expect[i] {
				t.Fatal("should have gotten GPU", expect[i])
			}
		case <-time.After(time.Millisecond * 100):
			t.Fatal("timeout")
		}
	}

	m := sync.Mutex{}
	list := []int{}
	for i := 0; i < len(expect); i++ {
		go func(i int) {
			ord := d.Get()
			m.Lock()
			list = append(list, ord)
			m.Unlock()
		}(i)
	}
	for i := range expect {
		d.Release(expect[i])
	}
	time.Sleep(time.Millisecond * 100)
	for i := range expect {
		if list[i] != i {
			t.Fatal("waitlist fired out-of-order", list, i)
		}
	}
}

func TestGlobal(t *testing.T) {
	ord := deviceOrdinalMgr.Get()
	deviceOrdinalMgr.Release(ord)
}
