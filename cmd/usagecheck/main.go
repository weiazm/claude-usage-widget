// Command usagecheck is the M1 smoke test: read the token and print current
// Claude usage to stdout. Run with: go run ./cmd/usagecheck
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
		fmt.Fprintln(os.Stderr, "auth:", err)
		os.Exit(1)
	}
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
