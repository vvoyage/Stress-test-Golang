package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	. "stress/common"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	Port      int
	Logger    *Logger
	LogFile   string
	RequestWG sync.WaitGroup
	Stats     *ServerStats
	done      chan struct{}
}

type ServerStats struct {
	mutex         sync.Mutex
	TotalRequests int
	Status200     int
	Status400     int
	TotalBytes    int64
	TotalDuration time.Duration
	MinDuration   time.Duration
	MaxDuration   time.Duration
}

func (s *ServerStats) RecordRequest(status int, duration time.Duration, bytes int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.TotalRequests++
	s.TotalDuration += duration
	s.TotalBytes += bytes

	if duration < s.MinDuration || s.MinDuration == 0 {
		s.MinDuration = duration
	}
	if duration > s.MaxDuration {
		s.MaxDuration = duration
	}

	switch status {
	case http.StatusOK:
		s.Status200++
	case http.StatusBadRequest:
		s.Status400++
	}
}

func (s *ServerStats) GetAndResetStats() (int, int, int, time.Duration, time.Duration, time.Duration, int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	totalRequests := s.TotalRequests
	status200 := s.Status200
	status400 := s.Status400
	avgDuration := time.Duration(0)
	if s.TotalRequests > 0 {
		avgDuration = time.Duration(int64(s.TotalDuration) / int64(s.TotalRequests))
	}
	minDuration := s.MinDuration
	maxDuration := s.MaxDuration
	totalBytes := s.TotalBytes

	// Сброс статистики
	s.TotalRequests = 0
	s.Status200 = 0
	s.Status400 = 0
	s.TotalDuration = 0
	s.MinDuration = 0
	s.MaxDuration = 0
	s.TotalBytes = 0

	return totalRequests, status200, status400, avgDuration, minDuration, maxDuration, totalBytes
}

func (s *Server) startStatsLogger(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			totalRequests, status200, status400, avgDuration, minDuration, maxDuration, totalBytes := s.Stats.GetAndResetStats()
			s.Logger.Info("Request statistics", Fields{
				"total_requests": totalRequests,
				"status_200":     status200,
				"status_400":     status400,
				"avg_duration":   avgDuration,
				"min_duration":   minDuration,
				"max_duration":   maxDuration,
				"total_bytes":    totalBytes,
			})
		case <-s.done:
			return
		}
	}
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
		Stats:   &ServerStats{},
		done:    make(chan struct{}),
	}, nil
}

func (s *Server) HandleSend(w http.ResponseWriter, r *http.Request) {
	s.RequestWG.Add(1)
	defer s.RequestWG.Done()

	startTime := time.Now()

	for _, name := range RequiredHeaders {
		if r.Header.Get(name) == "" {
			w.WriteHeader(http.StatusBadRequest)
			s.Logger.Error("Missing required header", Fields{
				"header": name,
				"status": http.StatusBadRequest,
			})
			s.Stats.RecordRequest(http.StatusBadRequest, time.Since(startTime), r.ContentLength)
			return
		}
	}

	_, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)

		s.Logger.Error("Error reading request body", Fields{
			"error":  err.Error(),
			"status": http.StatusInternalServerError,
		})
		s.Stats.RecordRequest(http.StatusInternalServerError, time.Since(startTime), r.ContentLength)
		return
	}
	defer r.Body.Close()

	s.Logger.Info("Request processed successfully", Fields{
		"status":          http.StatusOK,
		"missing_headers": &r.Header,
		"message_size":    r.ContentLength,
	})

	w.WriteHeader(http.StatusOK)
	s.Stats.RecordRequest(http.StatusOK, time.Since(startTime), r.ContentLength)
}

func (s *Server) Run() error {
	http.HandleFunc("/send/", s.HandleSend)

	s.Logger.Info("Starting server", Fields{
		"port": s.Port,
	})

	go s.startStatsLogger(5 * time.Millisecond) // Интервал можно настроить

	return http.ListenAndServe(fmt.Sprintf(":%d", s.Port), nil)
}

func (s *Server) Shutdown() {
	s.Logger.Info("Server shutting down. Waiting for active requests to complete...", nil)

	s.RequestWG.Wait()

	close(s.done)

	s.Logger.Info("Server shutdown complete", nil)

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
		server.Logger.Info("Received shutdown signal", Fields{
			"signal": sig.String(),
		})
		server.Shutdown()
	}()
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
		server.Logger.Error("Server error", Fields{
			"error": err.Error(),
		})
		server.Shutdown()
	}
}
