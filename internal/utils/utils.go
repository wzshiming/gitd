package utils

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
)

// Command creates an exec.Cmd with the given name and arguments, and logs the command being executed.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	slog.InfoContext(ctx, "Exec", "Cmd", name, "Args", args)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = os.Stderr
	return cmd
}
