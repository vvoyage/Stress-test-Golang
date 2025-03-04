package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	. "stress/common"
	"sync"
)

type Server struct {
	Port      int
	Logger    *Logger
	LogFile   string
	RequestWG sync.WaitGroup
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
	}, nil
}

func (s *Server) HandleSend(w http.ResponseWriter, r *http.Request) {
	s.RequestWG.Add(1)
	defer s.RequestWG.Done()

	for _, name := range RequiredHeaders {
		if r.Header.Get(name) == "" {
			w.WriteHeader(http.StatusBadRequest)
			s.Logger.Error("Missing required header", Fields{
				"header": name,
				"status": http.StatusBadRequest,
			})
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
		return
	}
	defer r.Body.Close()

	s.Logger.Info("Request processed successfully", Fields{
		"status":          http.StatusOK,
		"missing_headers": &r.Header,
		"message_size":    r.ContentLength,
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) Run() error {
	http.HandleFunc("/send/", s.HandleSend)

	s.Logger.Info("Starting server", Fields{
		"port": s.Port,
	})

	return http.ListenAndServe(fmt.Sprintf(":%d", s.Port), nil)
}

func (s *Server) Shutdown() {
	s.Logger.Info("Server shutting down. Waiting for active requests to complete...", nil)

	s.RequestWG.Wait()

	s.Logger.Info("Server shutdown complete", nil)

	if err := s.Logger.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing log file: %v\n", err)
	}
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

	err = server.Run()
	if err != nil {
		server.Logger.Error("Server error", Fields{
			"error": err.Error(),
		})
		server.Shutdown()
	}
}
