package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	. "stress/common"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type Server struct {
	Port      int
	Logger    *Logger
	LogFile   string
	RequestWG sync.WaitGroup
	Stats     *ServerStats
	done      chan struct{}
	mutex     sync.Mutex
}

type ServerStats struct {
	TotalRequests int
	StatusCodes   map[int]int
	TotalBytes    int64
	TotalDuration time.Duration
	MinDuration   time.Duration
	MaxDuration   time.Duration
	AvgDuration   time.Duration
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
	size       int64
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.size += int64(size)
	return size, err
}

func (s *Server) RequestStatsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.RequestWG.Add(1)
		defer s.RequestWG.Done()

		startTime := time.Now()

		recorder := &statusRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next(recorder, r)

		s.RecordRequest(recorder.statusCode, time.Since(startTime), r.ContentLength+recorder.size)
	}
}

func (s *Server) RecordRequest(status int, duration time.Duration, bytes int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.Stats.TotalRequests++
	s.Stats.TotalDuration += duration
	s.Stats.TotalBytes += bytes
	s.Stats.StatusCodes[status]++

	if duration < s.Stats.MinDuration || s.Stats.MinDuration == 0 {
		s.Stats.MinDuration = duration
	}
	if duration > s.Stats.MaxDuration {
		s.Stats.MaxDuration = duration
	}
}

func (s *Server) GetAndResetStats() ServerStats {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.Stats.TotalRequests > 0 {
		s.Stats.AvgDuration = time.Duration(int64(s.Stats.TotalDuration) / int64(s.Stats.TotalRequests))
	}

	snapshot := *s.Stats

	*s.Stats = ServerStats{
		StatusCodes: make(map[int]int),
	}

	return snapshot
}

func (s *Server) startStatsLogger(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := s.GetAndResetStats()
			s.Logger.Info().
				Interface("Statistics", stats).
				Msg("Request statistics")
		case <-s.done:
			return
		}
	}
}
func isValidHeader(header string, value []string) bool {
	switch header {
	case "x-esb-ver-id":
		err := uuid.Validate(value[0])
		return len(value) == 1 && err == nil
	case "x-esb-ver-no":
		_, err := time.Parse("20060102T150405", value[0])
		return len(value) == 1 && err == nil
	default:
		return true
	}
}

func isAuthenticated(value string) bool {
	return slices.Contains(EsbKeys[:], value)
}

func isValidHeader(header string, value []string) bool {
	switch header {
	case "x-esb-ver-id":
		err := uuid.Validate(value[0])
		return len(value) == 1 && err == nil
	case "x-esb-ver-no":
		_, err := time.Parse("20060102T150405", value[0])
		return len(value) == 1 && err == nil
	default:
		return true
	}
}

func isAuthenticated(value string) bool {
	return slices.Contains(EsbKeys[:], value)
}

func (s *Server) HandleSend(w http.ResponseWriter, r *http.Request) {
	s.RequestWG.Add(1)
	defer s.RequestWG.Done()

	for _, name := range RequiredHeaders {
		header := r.Header.Get(name)
		if header == "" {
			w.WriteHeader(http.StatusBadRequest)
			s.Logger.Error().
				Str("header", name).
				Int("status", http.StatusBadRequest).
				Msg("Missing required header")
			return
		}
	}

	esbKey := r.Header.Get("x-esb-key")
	if !isAuthenticated(esbKey) {
		w.WriteHeader(http.StatusForbidden)
		s.Logger.Error().
			Int("status", http.StatusForbidden).
			Msg("Not authenticated")
		return
	}

	for header, value := range r.Header {
		if !isValidHeader(header, value) {
			w.WriteHeader(http.StatusBadRequest)
			s.Logger.Error().
				Str("header", header).
				Int("status", http.StatusBadRequest).
				Msg("Not valid header")
			return
		}
	}

	esbKey := r.Header.Get("x-esb-key")
	if !isAuthenticated(esbKey) {
		w.WriteHeader(http.StatusForbidden)
		s.Logger.Error("Not authenticated", Fields{
			"status": http.StatusForbidden,
		})
		return
	}

	for header, value := range r.Header {
		if !isValidHeader(header, value) {
			w.WriteHeader(http.StatusBadRequest)
			s.Logger.Error("Not valid header", Fields{
				"header": header,
				"status": http.StatusBadRequest,
			})
			return
		}
	}

	_, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.Logger.Error().
			Err(err).
			Int("status", http.StatusInternalServerError).
			Msg("Error reading request body")
		return
	}
	defer r.Body.Close()

	s.Logger.Info().
		Int("status", http.StatusOK).
		Interface("headers", r.Header).
		Int64("message_size", r.ContentLength).
		Msg("Request processed successfully")

	w.WriteHeader(http.StatusOK)
}

func (s *Server) Run() error {
	http.HandleFunc("/send/", s.RequestStatsMiddleware(s.HandleSend))

	s.Logger.Info().
		Int("port", s.Port).
		Msg("Starting server")

	go s.startStatsLogger(5 * time.Second)

	return http.ListenAndServe(fmt.Sprintf(":%d", s.Port), nil)
}

func (s *Server) Shutdown() {
	s.Logger.Info().Msg("Server shutting down. Waiting for active requests to complete...")

	s.RequestWG.Wait()

	close(s.done)

	s.Logger.Info().Msg("Server shutdown complete")

	if err := s.Logger.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing log file: %v\n", err)
	}

	os.Exit(0)
}

func setupSignalHandler(server *Server) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChan
		server.Logger.Info().
			Str("signal", sig.String()).
			Msg("Received shutdown signal")
		server.Shutdown()
	}()
}

func NewServer(port int, logFile string) (*Server, error) {
	logger, err := NewLogger(logFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return &Server{
		Port:    port,
		Logger:  logger,
		LogFile: logFile,
		Stats: &ServerStats{
			StatusCodes: make(map[int]int),
		},
		done: make(chan struct{}),
	}, nil
}

func main() {
	port := flag.Int("port", 8080, "Server port")
	logFile := flag.String("log", "server.log", "Path to log file")
	flag.Parse()

	server, err := NewServer(*port, *logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating server: %v\n", err)
		os.Exit(1)
	}

	setupSignalHandler(server)

	err = server.Run()
	if err != nil {
		server.Logger.Error().
			Err(err).
			Msg("Error starting server")
		server.Shutdown()
	}
}
