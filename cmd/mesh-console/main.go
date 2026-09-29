// Command mesh-console is the local status and control page of a FOSS mesh
// box: IP and domain, how the gateways route to it, whether the template is up
// to date, and what the root domain points at. It sits behind an AppShield
// gate and serves an embedded Svelte UI plus a small JSON API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yundera/mesh-console/internal/config"
	"github.com/yundera/mesh-console/internal/server"
	"github.com/yundera/mesh-console/internal/ui"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("mesh-console: ")

	cfg := config.FromEnv()
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(cfg, ui.Dist()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("v%s listening on %s (mesh dir %s, host root %s)", server.Version, cfg.Addr, cfg.MeshDir, cfg.MeshHostRoot)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
