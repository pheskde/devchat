// Command devchatd is the devchat server: it hosts the SQLite-backed chat
// and key/value API and can install itself as a Windows service.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/kardianos/service"
)

func main() {
	fs := flag.NewFlagSet("devchatd", flag.ExitOnError)
	addr := fs.String("addr", envOr("DEVCHAT_ADDR", "0.0.0.0:8990"), "listen address")
	dbPath := fs.String("db", envOr("DEVCHAT_DB", defaultDBPath()), "path to the sqlite database file")

	// service.Control subcommands (install/uninstall/start/stop/restart) come
	// first and are not flags, so pull them off os.Args before flag parsing.
	var action string
	args := os.Args[1:]
	if len(args) > 0 && !isFlag(args[0]) {
		action = args[0]
		args = args[1:]
	}
	fs.Parse(args)

	if dir := filepath.Dir(*dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create db directory %q: %v", dir, err)
		}
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	svcConfig := &service.Config{
		Name:        "DevChat",
		DisplayName: "DevChat",
		Description: "devchat SQLite-backed chat server and LLM exchange API.",
		Arguments:   []string{"run", "-addr", *addr, "-db", *dbPath},
	}

	prg := &program{addr: *addr, dbPath: *dbPath, logger: logger}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatalf("create service: %v", err)
	}

	if action != "" && action != "run" {
		if err := service.Control(svc, action); err != nil {
			log.Fatalf("%s failed: %v", action, err)
		}
		fmt.Printf("%s: ok\n", action)
		return
	}

	// "run" (explicit or implicit): runs under the Windows service manager
	// when installed, or interactively in the foreground otherwise - the
	// same binary and flags work for local testing and as an installed
	// service.
	if err := svc.Run(); err != nil {
		logger.Fatalf("service run: %v", err)
	}
}

func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultDBPath() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("data", "devchat.db")
	}
	return filepath.Join(filepath.Dir(exe), "data", "devchat.db")
}
