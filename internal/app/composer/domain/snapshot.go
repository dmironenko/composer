package domain

// Snapshot identifies a committed, immutable repository tree.
type Snapshot struct {
	Commit string
	Root   string
}

// Report is the CLI-independent result of one GitOps pass.
type Report struct {
	Commits  map[string]string
	Commit   string
	Projects []Change
}
