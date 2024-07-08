package resources

import (
	"sync/atomic"

	"golang.org/x/xerrors"
)

type Ram uint64

var ramAvail atomic.Value

func init() {
	res, err := getResources()
	if err != nil {
		panic(err)
	}
	ramAvail.Store(res.Ram)
}

func (r Ram) HasCapacity() bool {
	return ramAvail.Load().(Ram) >= r
}

func (r Ram) Claim(taskID int) (func() error, error) {
	if !addToAvail(-int(r)) {
		return nil, xerrors.Errorf("not enough RAM available: < %d", r)
	}

	return func() error {
		impossibleErr := addToAvail(int(r))
		_ = impossibleErr
		return nil
	}, nil
}

func addToAvail(r int) bool {
	avail := ramAvail.Load().(Ram)
	if int(avail)+r < 0 {
		return false
	}
	for !ramAvail.CompareAndSwap(avail, Ram(int(avail)+r)) {
		avail = ramAvail.Load().(Ram)
		if int(avail)+r < 0 {
			return false
		}
	}
	return true
}
