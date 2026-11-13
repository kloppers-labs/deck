package launch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

var execCommand = exec.CommandContext

// Start runs argv and reports its output's last line when it fails.
func Start(ctx context.Context, argv []string) error {
	out, err := execCommand(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err == nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return fmt.Errorf("%s: %v: %s", argv[0], err, lines[len(lines)-1])
}
