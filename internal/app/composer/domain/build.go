// Package domain defines deployment inputs and results independently of infrastructure.
package domain

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// BuildSpec identifies all inputs to a source image. Runtime configs are not build inputs.
type BuildSpec struct {
	Repository string
	Ref        string
	Dockerfile string
}

// Image is a content-derived local Docker tag shared by identical builds.
func (s BuildSpec) Image() string {
	sum := sha256.Sum256([]byte(s.Repository + "\x00" + s.Ref + "\x00" + s.Dockerfile))

	return fmt.Sprintf("composer/build:%x", sum)
}

// ValidRef requires a nonempty revision; Git resolves it during the build.
func (s BuildSpec) ValidRef() bool {
	return strings.TrimSpace(s.Ref) != ""
}
