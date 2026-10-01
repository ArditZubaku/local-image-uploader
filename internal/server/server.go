// Package server provides the HTTP Server with logging middleware
// and local LAN detection functionalities
package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/handlers"
)

type Server struct {
	httpServer *http.Server
}

func New(cfg config.Config) *Server {
	mux := http.NewServeMux()

	handlers.Register(mux, cfg)

	httpSrv := &http.Server{
		Addr:    cfg.Addr,
		Handler: loggingMiddleware(mux),
		// No ReadTimeout/WriteTimeout: both would cap the whole request
		// lifetime, killing large uploads mid-transfer. ReadHeaderTimeout
		// alone still guards against slow-header (slowloris) connections.
		ReadHeaderTimeout: 15 * time.Second,
	}

	return &Server{httpServer: httpSrv}
}

func (s *Server) Start() error {
	// Shutdown on Ctrl+C
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	return s.httpServer.ListenAndServe()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s from %s in %s", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}
