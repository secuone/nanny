// Package queue is the task backlog nanny hands to Claude agents: tasks wait
// pending, run in an agent and end done or failed. Kept in
// $XDG_STATE_HOME/nanny/queue.json; the dispatcher lives in the UI.
package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/i18n"
)

// Modes say where a task runs.
const (
	Here     = "here"     // an idle nanny agent in the folder
	New      = "new"      // a new claude tab
	Worktree = "worktree" // a new git worktree, then claude in it
)

// Modes in the order the form cycles them.
var Modes = []string{Here, New, Worktree}

// Statuses.
const (
	Pending = "pending"
	Running = "running"
	Done    = "done"
	Failed  = "failed"
)

// StartGrace is how long a running task may sit idle without ever working
// before it counts as done (it finished between two looks).
const StartGrace = 45 * time.Second

type Task struct {
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"` // tab label and branch name
	Prompt  string    `json:"prompt"`
	Dir     string    `json:"dir"`
	Mode    string    `json:"mode"`
	Status  string    `json:"status"`
	Session string    `json:"session,omitempty"` // Claude session it went to
	Where   string    `json:"where,omitempty"`   // space/tab it went to
	Busy    bool      `json:"busy,omitempty"`    // its agent was seen working
	Err     string    `json:"error,omitempty"`
	Added   time.Time `json:"added"`
	Started time.Time `json:"started,omitempty"`
	Ended   time.Time `json:"ended,omitempty"`
}

type Queue struct {
	Auto  bool    `json:"auto"` // dispatch pending tasks by themselves
	Tasks []*Task `json:"tasks"`
}

var seq atomic.Int64

// NewTask is a pending task.
func NewTask(prompt, dir, mode, name string) *Task {
	id := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(seq.Add(1), 36)
	return &Task{ID: id, Prompt: prompt, Dir: dir, Mode: mode, Name: name, Status: Pending, Added: time.Now()}
}

func Path() string { return filepath.Join(config.StateDir(), "queue.json") }

// Load reads the queue; a missing file is an empty queue.
func Load() (Queue, error) {
	var q Queue
	b, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return q, nil
	}
	if err != nil {
		return q, err
	}
	err = json.Unmarshal(b, &q)
	return q, err
}

// Save writes the queue atomically.
func Save(q Queue) error {
	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// Find returns the task with id, or nil.
func (q *Queue) Find(id string) *Task {
	for _, t := range q.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// Remove drops the task with id.
func (q *Queue) Remove(id string) {
	for i, t := range q.Tasks {
		if t.ID == id {
			q.Tasks = append(q.Tasks[:i], q.Tasks[i+1:]...)
			return
		}
	}
}

// Move swaps a pending task with the next pending one up (d < 0) or down.
func (q *Queue) Move(id string, d int) bool {
	i := -1
	for j, t := range q.Tasks {
		if t.ID == id {
			i = j
		}
	}
	if i < 0 || q.Tasks[i].Status != Pending {
		return false
	}
	for j := i + d; j >= 0 && j < len(q.Tasks); j += d {
		if q.Tasks[j].Status == Pending {
			q.Tasks[i], q.Tasks[j] = q.Tasks[j], q.Tasks[i]
			return true
		}
	}
	return false
}

// Count is how many tasks have status st.
func (q *Queue) Count(st string) int {
	n := 0
	for _, t := range q.Tasks {
		if t.Status == st {
			n++
		}
	}
	return n
}

// Room is how many more tasks may start with max running at once.
func (q *Queue) Room(max int) int { return max - q.Count(Running) }

// Pending lists the waiting tasks in queue order.
func (q *Queue) Pending() []*Task {
	var out []*Task
	for _, t := range q.Tasks {
		if t.Status == Pending {
			out = append(out, t)
		}
	}
	return out
}

// Start marks a task running.
func (t *Task) Start(where string, now time.Time) {
	t.Status, t.Where, t.Started, t.Busy, t.Err, t.Ended = Running, where, now, false, "", time.Time{}
}

// Fail ends a task as failed.
func (t *Task) Fail(err string, now time.Time) {
	t.Status, t.Err, t.Ended = Failed, err, now
}

// Retry puts a failed or done task back in line.
func (t *Task) Retry() {
	t.Status, t.Err, t.Session, t.Where, t.Busy = Pending, "", "", "", false
	t.Started, t.Ended = time.Time{}, time.Time{}
}

// Advance moves a running task along with its agent's state: "working" or
// "blocked" (busy), "idle", "starting" (no agent yet, wait), "exited" or ""
// (gone). It reports whether the task ended.
func (t *Task) Advance(state string, now time.Time) bool {
	if t.Status != Running {
		return false
	}
	switch state {
	case "starting":
		return false
	case "working", "blocked":
		t.Busy = true
		return false
	case "idle":
		if !t.Busy && now.Sub(t.Started) < StartGrace {
			return false
		}
		t.Status, t.Ended = Done, now
		return true
	}
	if t.Busy {
		t.Status, t.Ended = Done, now
		return true
	}
	t.Fail(i18n.M("agent exited"), now)
	return true
}

// Step advances every running task with state and returns those that ended.
func (q *Queue) Step(state func(*Task) string, now time.Time) []*Task {
	var ended []*Task
	for _, t := range q.Tasks {
		if t.Status == Running && t.Advance(state(t), now) {
			ended = append(ended, t)
		}
	}
	return ended
}

// Idle reports whether nothing is pending or running.
func (q *Queue) Idle() bool { return q.Count(Pending) == 0 && q.Count(Running) == 0 }
