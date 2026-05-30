// Command usagecheck is a small command-line self-check: read the token and
// print current Claude usage to stdout. Run with: go run ./cmd/usagecheck
//
// Go note: this is a *second* program in the same module. Each folder under
// cmd/ with its own "package main" + func main() builds into its own binary,
// while sharing the internal/ libraries with the tray app.
package main

import (
	"context"
	"fmt"
	"os"

	"claude-usage-widget/internal/auth"
	"claude-usage-widget/internal/usage"
)

func main() {
	tok, err := auth.Read()
	if err != nil {
		// Go note: os.Stderr is the standard error stream; os.Exit(1) ends the
		// program with a non-zero status so scripts know it failed.
		fmt.Fprintln(os.Stderr, "auth:", err)
		os.Exit(1)
	}
	// context.Background() is the empty root context — fine for a one-shot CLI
	// run with no deadline of its own (Fetch applies its own timeout).
	u, err := usage.Fetch(context.Background(), tok.AccessToken)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		os.Exit(1)
	}
	fmt.Println("Claude Usage")
	fmt.Println("────────────────────────────")
	fmt.Println(u.Summary())
	if u.ExtraUsage.IsEnabled {
		fmt.Println("额外用量: 已开启")
	}
}
