// Package task holds the domain model of a smate task: statuses and the
// meta.json schema.
package task

import "time"

// Status is the state of a task in the control plane.
type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDone     Status = "DONE"
	StatusRejected Status = "REJECTED"
	StatusCleaned  Status = "CLEANED"
)

// Task is the content of ~/.smate/tasks/<id>/meta.json.
type Task struct {
	ID   string `json:"id"`
	Repo string `json:"repo"` // absolute path to the working repository

	// Branch and BaseSHA are the repository branch and its HEAD at start time.
	// BaseSHA is the base the snapshot was taken from and what the second
	// apply guard compares against.
	Branch  string `json:"branch"`
	BaseSHA string `json:"base_sha"`

	// Baseline is the first commit of the throwaway repository inside the
	// workspace and the base for format-patch. Not to be confused with BaseSHA.
	Baseline string `json:"baseline"`

	// AppliedHead is where the task's branch stood after the last successful
	// import. It is how apply tells its own previous import, which it may
	// replace, from a branch someone has since committed on, which it may not.
	AppliedHead string `json:"applied_head,omitempty"`

	// Secrets are the paths cut at start, kept here rather than re-read from
	// .smate.yml so that editing the config cannot change the rules for a
	// series already produced.
	Secrets []string `json:"secrets,omitempty"`

	Image     string    `json:"image"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (t Task) Container() string { return "smate-" + t.ID }
