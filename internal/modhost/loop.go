package modhost

import (
	"sync"
	"time"
)

// loop runs jobs one at a time on its own goroutine: the only goroutine
// that touches the VM. Other goroutines (the reader, timers) post jobs.
// The queue is unbounded; the server's per-mod queue bounds what arrives.
type loop struct {
	mu   sync.Mutex
	jobs []func()
	wake chan struct{}
	done chan struct{}
	stop bool
}

func newLoop() *loop {
	return &loop{wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// post queues f. It returns false once the loop has stopped.
func (l *loop) post(f func()) bool {
	l.mu.Lock()
	if l.stop {
		l.mu.Unlock()
		return false
	}
	l.jobs = append(l.jobs, f)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
	return true
}

// run runs jobs until quit is called. No job runs after the one that
// called quit.
func (l *loop) run() {
	defer close(l.done)
	for {
		l.mu.Lock()
		if l.stop {
			l.mu.Unlock()
			return
		}
		if len(l.jobs) == 0 {
			l.mu.Unlock()
			<-l.wake
			continue
		}
		f := l.jobs[0]
		l.jobs[0] = nil
		l.jobs = l.jobs[1:]
		l.mu.Unlock()
		f()
	}
}

// quit stops the loop after the running job.
func (l *loop) quit() {
	l.mu.Lock()
	l.stop = true
	l.jobs = nil
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// timer is one setTimeout or setInterval. Fields are touched only on the
// loop goroutine.
type timer struct {
	t     *time.Timer
	every time.Duration // 0 for setTimeout
}
