package util

import "time"

// Stopwatch is a simple accumulative timer that can be started, paused,
// and resumed. Elapsed() returns the total accumulated time regardless
// of whether the watch is currently running.
type Stopwatch struct {
	running     bool
	start       time.Time
	accumulated time.Duration
}

// Start begins (or restarts) the stopwatch, resetting accumulated time to zero.
func (sw *Stopwatch) Start() {
	sw.running = true
	sw.start = time.Now()
	sw.accumulated = 0
}

// Pause stops the stopwatch and accumulates the elapsed time so far.
// Subsequent calls to Pause() have no effect until Start() or Resume() is called.
func (sw *Stopwatch) Pause() {
	if !sw.running {
		return
	}
	sw.accumulated += time.Since(sw.start)
	sw.running = false
}

// Resume restarts a paused stopwatch, continuing from where it left off.
func (sw *Stopwatch) Resume() {
	if sw.running {
		return
	}
	sw.running = true
	sw.start = time.Now()
}

// Elapsed returns the total time accumulated across all running periods.
// If currently running, includes time since the last Start()/Resume().
func (sw *Stopwatch) Elapsed() time.Duration {
	if sw.running {
		return sw.accumulated + time.Since(sw.start)
	}
	return sw.accumulated
}

// IsRunning reports whether the stopwatch is currently ticking.
func (sw *Stopwatch) IsRunning() bool {
	return sw.running
}
