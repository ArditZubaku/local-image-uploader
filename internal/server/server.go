// Package server provides the HTTP Server with logging middleware
// and local LAN detection functionalities
package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

// progressEvery bounds how often a transfer in flight reports in. A phone
// pulling a 17 GB folder would otherwise leave the log silent for twenty
// minutes with no way to tell a slow link from a stalled one.
const progressEvery = 5 * time.Second

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rec := &recorder{ResponseWriter: w, req: r, start: start, status: http.StatusOK}
		body := &countingBody{ReadCloser: r.Body, rec: rec}
		r.Body = body

		next.ServeHTTP(rec, r)

		elapsed := time.Since(start)

		// Uploads move their bytes through the request body and reply with
		// a few bytes of JSON, so reporting the response size alone would
		// say nothing about the transfer that just happened.
		moved, label := rec.written, ""
		if body.read > rec.written {
			moved, label = body.read, "up "
		}

		size := utils.FormatBytes(moved)
		// A client that disappears mid-stream leaves the status at 200,
		// which is how a cancelled 17 GB download used to look exactly
		// like a successful one.
		if declared := declaredLength(rec.Header()); declared > 0 && r.Method != http.MethodHead && rec.written < declared {
			size = fmt.Sprintf("%s/%s INCOMPLETE", utils.FormatBytes(rec.written), utils.FormatBytes(declared))
		}

		log.Printf("%s %s from %s -> %d %s%s in %s%s",
			r.Method, r.URL.RequestURI(), r.RemoteAddr, rec.status,
			label, size, elapsed.Round(time.Millisecond), rateOf(moved, elapsed))
	})
}

// declaredLength reads the Content-Length a handler set, or -1 when it
// left the size open.
func declaredLength(h http.Header) int64 {
	v := h.Get("Content-Length")
	if v == "" {
		return -1
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func rateOf(n int64, d time.Duration) string {
	if n <= 0 || d <= 0 {
		return ""
	}
	return " @ " + utils.FormatBytes(int64(float64(n)/d.Seconds())) + "/s"
}

func etaOf(done, total int64, d time.Duration) string {
	if done <= 0 || done >= total || d <= 0 {
		return ""
	}
	rate := float64(done) / d.Seconds()
	if rate <= 0 {
		return ""
	}
	left := time.Duration(float64(total-done) / rate * float64(time.Second))
	return ", " + left.Round(time.Second).String() + " left"
}

type recorder struct {
	http.ResponseWriter
	req     *http.Request
	start   time.Time
	status  int
	written int64

	lastTick time.Time
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.written += int64(n)
	r.tick(r.written, declaredLength(r.Header()), "sent")
	return n, err
}

// ReadFrom keeps http.ServeFile's sendfile path reachable: without it the
// wrapper hides the underlying ReaderFrom and every file download falls
// back to copying through userspace. The whole file moves inside one call,
// so these transfers report only on completion.
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

// tick logs an in-flight transfer at most every progressEvery, and only
// when the total is known - a percentage is the point of the line.
func (r *recorder) tick(done, total int64, verb string) {
	if total <= 0 {
		return
	}
	if r.lastTick.IsZero() {
		r.lastTick = r.start
	}
	if time.Since(r.lastTick) < progressEvery {
		return
	}
	r.lastTick = time.Now()

	elapsed := time.Since(r.start)
	log.Printf("  ... %s %s %s %s of %s (%d%%)%s%s",
		r.req.Method, r.req.URL.RequestURI(), verb,
		utils.FormatBytes(done), utils.FormatBytes(total), done*100/total,
		rateOf(done, elapsed), etaOf(done, total, elapsed))
}

// countingBody measures an upload as it is read, so a long POST reports
// progress the same way a long download does.
type countingBody struct {
	io.ReadCloser
	rec  *recorder
	read int64
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.read += int64(n)
	c.rec.tick(c.read, c.rec.req.ContentLength, "received")
	return n, err
}
