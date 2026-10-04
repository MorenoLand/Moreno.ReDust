package scripts

import "errors"

// ScriptTask runs script work that may block the way native commands block
// inside their message pump: puppetspeak until its line ends, puppetevent
// until a choice is clicked, delay for a number of ticks, forceupdate for a
// scheduler pass. The work runs on its own goroutine but strictly alternates
// with the game loop: exactly one of them runs at any moment, so script and
// game state are never touched concurrently.
type ScriptTask struct {
	resume  chan struct{}
	paused  chan struct{}
	ready   func() bool
	running bool
	done    bool
	err     error
	// Label describes the work for diagnostics.
	Label string
}

// ErrNotInTask reports a blocking command reached outside a task, such as
// from an event raised while another task is suspended.
var ErrNotInTask = errors.New("blocking script command outside a script task")

// StartScriptTask runs work until it first waits or finishes.
func StartScriptTask(label string, work func(task *ScriptTask) error) *ScriptTask {
	task := &ScriptTask{resume: make(chan struct{}), paused: make(chan struct{}), Label: label}
	go func() {
		<-task.resume
		task.err = work(task)
		task.done, task.running = true, false
		task.paused <- struct{}{}
	}()
	task.step()
	return task
}

func (t *ScriptTask) step() {
	t.running = true
	t.resume <- struct{}{}
	<-t.paused
}

// Running reports whether the caller is the task's own goroutine, the only
// place Wait may be called.
func (t *ScriptTask) Running() bool { return t != nil && t.running && !t.done }

// Wait suspends the task until ready reports true. ready is evaluated on the
// game loop's goroutine while the task is suspended.
func (t *ScriptTask) Wait(ready func() bool) {
	if ready() {
		return
	}
	t.ready = ready
	t.running = false
	t.paused <- struct{}{}
	<-t.resume
}

// Waiting reports whether the task is suspended on a condition.
func (t *ScriptTask) Waiting() bool { return t != nil && !t.done && t.ready != nil }

// Poll is called by the game loop. It resumes the task when its condition
// holds and reports whether the work has finished and with what error.
func (t *ScriptTask) Poll() (bool, error) {
	if t.done {
		return true, t.err
	}
	if t.ready != nil && !t.ready() {
		return false, nil
	}
	t.ready = nil
	t.step()
	return t.done, t.err
}
