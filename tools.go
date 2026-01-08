//go:build tools

// This file declares dependencies on tool binaries used for development.
// It ensures they are tracked in go.mod and available for go install.
package tools

import (
	_ "github.com/git-lfs/git-lfs/v3"
)
