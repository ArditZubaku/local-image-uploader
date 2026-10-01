// Package server provides the HTTP Server with logging middleware
// and local LAN detection functionalities
package server

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/handlers"
	"github.com/ArditZubaku/go-local-image-uploader/internal/utils"
)

type Server struct {
	httpServer *http.Server
}

func New(cfg config.Config) (*Server, error) {
	mux := http.NewServeMux()

	if err := handlers.Register(mux, cfg); err != nil {
		return nil, err
	}

	httpSrv := &http.Server{
		Addr:    cfg.Addr,
		Handler: loggingMiddleware(mux),
		// No ReadTimeout/WriteTimeout: both would cap the whole request
		// lifetime, killing large uploads mid-transfer. ReadHeaderTimeout
		// alone still guards against slow-header (slowloris) connections.
		ReadHeaderTimeout: 15 * time.Second,
		// The upload page keeps several connections open at once; this
		// reaps the ones it stops using instead of leaving them parked.
		IdleTimeout: 2 * time.Minute,
	}

	return &Server{httpServer: httpSrv}, nil
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
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		elapsed := time.Since(start)
		// Throughput is the number worth seeing in a transfer tool: it is
		// the only way to tell a slow phone from a slow network.
		rate := ""
		if rec.written > 0 && elapsed > 0 {
			rate = " @ " + utils.FormatBytes(int64(float64(rec.written)/elapsed.Seconds())) + "/s"
		}

		log.Printf("%s %s from %s -> %d %s in %s%s",
			r.Method, r.URL.Path, r.RemoteAddr, rec.status,
			utils.FormatBytes(rec.written), elapsed.Round(time.Millisecond), rate)
	})
}

type recorder struct {
	http.ResponseWriter
	status  int
	written int64
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.written += int64(n)
	return n, err
}

// ReadFrom keeps http.ServeFile's sendfile path reachable: without it the
// wrapper hides the underlying ReaderFrom and every file download falls
// back to copying through userspace.
func (r *recorder) ReadFrom(src io.Reader) (int64, error) {
	rf, ok := r.ResponseWriter.(io.ReaderFrom)
	if !ok {
		n, err := io.Copy(r.ResponseWriter, src)
		r.written += n
		return n, err
	}

	n, err := rf.ReadFrom(src)
	r.written += n
	return n, err
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
