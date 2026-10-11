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
	Tag        string
	Dockerfile string
}

// Image is a content-derived local Docker tag shared by identical builds.
func (s BuildSpec) Image() string {
	sum := sha256.Sum256([]byte(s.Repository + "\x00" + s.Tag + "\x00" + s.Dockerfile))

	return fmt.Sprintf("composer/build:%x", sum)
}

// ValidTag requires a nonempty revision; Git resolves it during the build.
func (s BuildSpec) ValidTag() bool {
	return strings.TrimSpace(s.Tag) != ""
}
