// Package domain defines deployment inputs and results independently of infrastructure.
package domain

import (
	"crypto/sha256"
	"fmt"
	"regexp"
)

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// BuildSpec identifies all inputs to a source image. Runtime configs are not build inputs.
type BuildSpec struct {
	Repository string
	Commit     string
	Dockerfile string
}

// Image is a content-derived local Docker tag shared by identical builds.
func (s BuildSpec) Image() string {
	sum := sha256.Sum256([]byte(s.Repository + "\x00" + s.Commit + "\x00" + s.Dockerfile))

	return fmt.Sprintf("composer/build:%x", sum)
}

// ValidCommit requires an immutable full lowercase Git SHA-1 revision.
func (s BuildSpec) ValidCommit() bool {
	return commitPattern.MatchString(s.Commit)
}
