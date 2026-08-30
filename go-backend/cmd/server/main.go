// Command server runs the coder-service Go backend: a JSON API that
// authenticates users against Coder and exposes workspace helpers. It is a
// 1:1 port of the Python backend in backend/app.
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

	"github.com/malmonte/go-backend/internal/api"
	"github.com/malmonte/go-backend/internal/config"
)

func main() {
	settings := config.Load()
	app := api.NewApp(settings)

	server := &http.Server{
		Addr:              settings.ListenAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("go-backend listening on %s (coder: %s)", settings.ListenAddr, settings.CoderAPIBase())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Fail the readiness probe first so load balancers drain traffic, then
	// shut down gracefully.
	app.SetReady(false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
