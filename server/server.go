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

func IsValidHeader(headerName string, header string) bool {
	things := []string{"foo", "bar", "baz"}
	slices.Contains(things, "foo") // true
	switch headerName {
	case "x-esb-key":
		return slices.Contains(EsbKeys[:], header)
	case "x-esb-ver-id":
		err := uuid.Validate(header)
		return err == nil
	case "x-esb-ver-no":
		_, err := time.Parse("20060102T150405", header)
		return err == nil
	default:
		return true
	}
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
		} else if !IsValidHeader(name, r.Header.Get(name)) {
			w.WriteHeader(http.StatusBadRequest)
			s.Logger.Error("Not valid header", Fields{
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
