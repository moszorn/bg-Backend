package project

import "sync/atomic"

type waitOnce struct {
	locked *uint32
	ready  *uint32

	err     error
	channel chan struct{}
}

func newWaiterOnce() *waitOnce {
	return &waitOnce{
		locked:  new(uint32),
		ready:   new(uint32),
		channel: make(chan struct{}),
	}
}

func (w *waitOnce) isReady() bool {
	// why ?
	if w == nil {
		return true
	}
	return atomic.LoadUint32(w.ready) > 0
}

func (w *waitOnce) wait() error {
	if w == nil {
		return nil
	}
	if w.isReady() {
		return w.err
	}
	if atomic.CompareAndSwapUint32(w.locked, 0, 1) {
		//開始等待
		<-w.channel
	}
	return w.err
}

func (w *waitOnce) unwait(err error) {
	if w == nil || w.isReady() {
		return
	}

	w.err = err
	atomic.StoreUint32(w.ready, 1)

	if atomic.CompareAndSwapUint32(w.locked, 1, 0) {
		close(w.channel)
	}
}
