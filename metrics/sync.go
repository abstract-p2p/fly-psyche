package metrics

import (
	"sync"
	"time"
)

type OncePerDur struct {
	dur time.Duration

	mu   sync.Mutex
	last time.Time

	// If the timer is running then pending will be non-nil, but
	// the converse is not always true: if pending is non-nil then
	// either the timer or the function runner is running.
	timer   *time.Timer
	pending func()

	// isRunning is true if and only if the function runner is running.
	// pending may or may not be nil, regardless of whether isRunning is
	// true or false.
	isRunning bool
}

func NewOncePerDur(dur time.Duration) *OncePerDur {
	return &OncePerDur{
		dur: dur,
	}
}

func (op *OncePerDur) Do(f func()) {
	op.mu.Lock()

	// if timer or timer handler is already running,
	// just update the pending func
	if op.pending != nil || op.isRunning {
		op.pending = f
		op.mu.Unlock()
		return
	}

	// if not enough time has passed, schedule f to be called later
	now := time.Now()
	next := op.last.Add(op.dur)
	if now.Before(next) {
		op.pending = f

		if op.timer == nil {
			op.timer = time.AfterFunc(now.Sub(next), op.handleTimer)
		} else {
			op.timer.Reset(op.dur)
		}

		op.mu.Unlock()
		return
	}

	// otherwise just run f
	op.last = now
	op.isRunning = true
	op.mu.Unlock()

	go op.runner(f)
}

// Reset stops any running timers and scheduled functions,
// and sets the last run time to now.
func (op *OncePerDur) Reset() {
	op.mu.Lock()
	if op.timer != nil {
		op.timer.Stop()
	}
	op.pending = nil
	op.last = time.Now()
	op.mu.Unlock()
}

func (op *OncePerDur) runner(f func()) {
	f()

	// If f is slow to execute or op.Dur is small, new fs will
	// be scheduled before f completes, so we'll just run them
	// in a loop.
	//
	// An alternative to this would be to start every f in a
	// new goroutine, but for our Gauge use case we don't want
	// multiple fs to be running concurrently if Dur is small.
	for {
		op.mu.Lock()
		if op.pending == nil {
			op.isRunning = false
			op.mu.Unlock()
			return
		}

		op.last = time.Now()
		f := op.pending
		op.pending = nil

		op.mu.Unlock()

		f()
	}
}

func (op *OncePerDur) handleTimer() {
	op.mu.Lock()

	if op.pending == nil {
		op.mu.Unlock()
		return
	}

	op.last = time.Now()
	f := op.pending
	op.pending = nil
	op.isRunning = true

	op.mu.Unlock()

	op.runner(f)
}

type OncePerDelta struct {
	delta int64

	mu      sync.Mutex
	lastVal int64
}

func NewOncePerDelta(delta int64) *OncePerDelta {
	return &OncePerDelta{
		delta: delta,
	}
}

func (opd *OncePerDelta) Do(val int64, fn func()) {
	opd.mu.Lock()

	// take absolute difference
	diff := val - opd.lastVal
	if diff < 0 {
		diff = -diff
	}

	if diff < opd.delta {
		opd.mu.Unlock()
		return
	}

	opd.lastVal = val
	opd.mu.Unlock()

	fn()
}

// Reset just sets the lastVal
func (opd *OncePerDelta) Reset(val int64) {
	opd.mu.Lock()
	opd.lastVal = val
	opd.mu.Unlock()
}
