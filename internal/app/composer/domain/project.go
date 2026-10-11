package domain

import "errors"

// ErrInvalid identifies invalid desired project input.
var ErrInvalid = errors.New("invalid project")

// Project is the desired state of one independent Compose project.
// Invalid inputs stay in the inventory with Error set, never as deletions.
type Project struct {
	Name        string
	Environment string
	Enabled     bool
	Hash        string
	Error       string
	Network     string
	Networks    []string
	Directory   string
	Manifest    Manifest
	Build       *BuildSpec
}

// Change combines desired state with its planned operation and eventual outcome.
type Change struct {
	Project

	Action string
}

// Manifest carries an opaque rendered Compose document without exposing the SDK model.
type Manifest struct {
	Content    []byte
	WorkingDir string
	ConfigHash string
}
