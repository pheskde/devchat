package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/pheskde/devchat/internal/api"
	"github.com/pheskde/devchat/internal/store"

	"github.com/kardianos/service"
)

// program implements service.Interface so devchatd can run both as an
// installed Windows service and as a plain foreground process (handy for
// local development).
type program struct {
	addr   string
	dbPath string
	logger *log.Logger

	httpSrv *http.Server
	st      *store.Store
}

func (p *program) Start(s service.Service) error {
	// Start must not block; the actual work happens in run().
	go p.run()
	return nil
}

func (p *program) run() {
	ctx := context.Background()

	st, err := store.Open(ctx, p.dbPath)
	if err != nil {
		p.logger.Fatalf("open store at %q: %v", p.dbPath, err)
	}
	p.st = st

	apiSrv := api.New(st, p.logger)
	p.httpSrv = &http.Server{
		Addr:    p.addr,
		Handler: apiSrv.Handler(),
	}

	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		p.logger.Fatalf("listen on %q: %v", p.addr, err)
	}

	p.logger.Printf("devchatd listening on http://%s (db: %s)", p.addr, p.dbPath)
	if err := p.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		p.logger.Printf("http server error: %v", err)
	}
}

func (p *program) Stop(s service.Service) error {
	p.logger.Printf("devchatd shutting down")
	if p.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.httpSrv.Shutdown(ctx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
	}
	if p.st != nil {
		return p.st.Close()
	}
	return nil
}
