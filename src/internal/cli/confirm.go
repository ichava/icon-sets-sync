package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

// confirm asks a yes/no question. --yes answers it; without a terminal, or
// under --no-input, it is a usage error rather than a hang, so a CI job fails
// loudly instead of waiting on input that never comes.
func confirm(question string) error {
	if ui.AssumeYes() {
		return nil
	}
	if !ui.Interactive() {
		return usageErr("%s -- pass --yes to confirm without a terminal", question)
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a == "y" || a == "yes" {
		return nil
	}
	return failure(fmt.Errorf("not confirmed; nothing was changed"))
}
