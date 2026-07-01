// Command devchat-viewer is a terminal UI that live-follows devchat
// sessions served by devchatd.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	apiBase := flag.String("api", envOr("DEVCHAT_API", "http://127.0.0.1:8990"), "devchatd API base URL")
	flag.Parse()

	cl := newClient(*apiBase)
	p := tea.NewProgram(newModel(cl), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "devchat-viewer:", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
