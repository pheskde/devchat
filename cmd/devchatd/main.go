// Command devchatd is the devchat server: it hosts the SQLite-backed chat
// and key/value API and can install itself as a Windows service.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/kardianos/service"
)

func main() {
	fs := flag.NewFlagSet("devchatd", flag.ExitOnError)
	addr := fs.String("addr", envOr("DEVCHAT_ADDR", "0.0.0.0:8990"), "listen address")
	dbPath := fs.String("db", envOr("DEVCHAT_DB", defaultDBPath()), "path to the sqlite database file")
	token := fs.String("token", os.Getenv("DEVCHAT_TOKEN"), "bearer token every client must send (env DEVCHAT_TOKEN)")
	tokenFile := fs.String("token-file", os.Getenv("DEVCHAT_TOKEN_FILE"), "file holding the bearer token (env DEVCHAT_TOKEN_FILE); preferred for services")
	insecure := fs.Bool("insecure-no-auth", false, "allow running without a token on a non-loopback address")

	// service.Control subcommands (install/uninstall/start/stop/restart) come
	// first and are not flags, so pull them off os.Args before flag parsing.
	var action string
	args := os.Args[1:]
	if len(args) > 0 && !isFlag(args[0]) {
		action = args[0]
		args = args[1:]
	}
	fs.Parse(args)

	tok, err := resolveToken(*token, *tokenFile)
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	if tok == "" && !*insecure && !isLoopbackAddr(*addr) {
		log.Fatalf("refusing to listen on %q without a token: set -token-file/-token (or DEVCHAT_TOKEN), or pass -insecure-no-auth", *addr)
	}

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
		Arguments:   svcArgs(*addr, *dbPath, *tokenFile, *insecure),
	}

	prg := &program{addr: *addr, dbPath: *dbPath, token: tok, logger: logger}
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

// resolveToken returns the token from -token-file if given, else -token. The
// token is never part of the service's command line (visible to every local
// user), which is why installed services should use -token-file.
func resolveToken(token, file string) (string, error) {
	if file == "" {
		return strings.TrimSpace(token), nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func svcArgs(addr, dbPath, tokenFile string, insecure bool) []string {
	args := []string{"run", "-addr", addr, "-db", dbPath}
	if tokenFile != "" {
		if abs, err := filepath.Abs(tokenFile); err == nil {
			tokenFile = abs
		}
		args = append(args, "-token-file", tokenFile)
	}
	if insecure {
		args = append(args, "-insecure-no-auth")
	}
	return args
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
