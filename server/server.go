package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	. "stress/common"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type HttpSession struct {
	httpClient http.Client
	useSession bool
	ibSession  string
	usr        string
	pwd        string
	path       string
}

func NewSession(path, usr, pwd string, useSession bool) *HttpSession {
	session := &HttpSession{
		httpClient: http.Client{
			Timeout:   1 * time.Second,
			Transport: &http.Transport{},
		},
		useSession: useSession,
		ibSession:  "",
		usr:        usr,
		pwd:        pwd,
		path:       path,
	}
	return session
}

func (s *HttpSession) NewSession() error {
	r, _ := http.NewRequest(http.MethodGet, s.path, strings.NewReader(""))
	r.Header.Add("IBSession", "start")
	if (s.usr != "") && (s.pwd != "") {
		r.SetBasicAuth(s.usr, s.pwd)
	}
	resp, errReq := s.httpClient.Do(r)
	if (errReq == nil) && (resp.StatusCode == 200) {
		s.ibSession = ""
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "ibsession" {
				s.ibSession = cookie.Value
				break
			}
		}
	} else {
		s.ibSession = ""
		return errReq
	}

	return nil
}

func (s *HttpSession) TrySend(method string, path string, body string, headers http.Header) (*http.Response, error) {
	resp, err := http.NewRequest(method, path, strings.NewReader(body))

	if err != nil {
		return nil, err
	}
	for header, values := range headers {
		for _, value := range values {
			resp.Header.Add(header, value)
		}
	}
	if (s.usr != "") && (s.pwd != "") {
		resp.SetBasicAuth(s.usr, s.pwd)
	}
	if s.useSession && s.ibSession == "" {
		err := s.NewSession()
		if err != nil {
			return nil, err
		}
	}
	if s.useSession && s.ibSession != "" {
		resp.AddCookie(&http.Cookie{Name: "ibsession", Value: s.ibSession})
	}
	return s.httpClient.Do(resp)
}

type Server struct {
	Port      int
	Logger    *Logger
	LogFile   string
	RequestWG sync.WaitGroup
	//Stats                *ServerStats
	done      chan struct{}
	taskQueue chan *requestTask
	//mutex                sync.Mutex
	authenticateRequests bool
}

//type ServerStats struct {
//	TotalRequests int
//	StatusCodes   map[int]int
//	TotalBytes    int64
//	TotalDuration time.Duration
//	MinDuration   time.Duration
//	MaxDuration   time.Duration
//	AvgDuration   time.Duration
//}
//
//type statusRecorder struct {
//	http.ResponseWriter
//	statusCode int
//}
//
//func (r *statusRecorder) WriteHeader(statusCode int) {
//	r.statusCode = statusCode
//	r.ResponseWriter.WriteHeader(statusCode)
//}
//
//func (s *Server) RequestStatsMiddleware(next http.HandlerFunc) http.HandlerFunc {
//	return func(w http.ResponseWriter, r *http.Request) {
//		s.RequestWG.Add(1)
//		defer s.RequestWG.Done()
//
//		startTime := time.Now()
//
//		recorder := &statusRecorder{
//			ResponseWriter: w,
//			statusCode:     http.StatusOK,
//		}
//
//		next(recorder, r)
//
//		s.RecordRequest(recorder.statusCode, time.Since(startTime), r.ContentLength)
//	}
//}
//
//func (s *Server) RecordRequest(status int, duration time.Duration, bytes int64) {
//	s.mutex.Lock()
//	defer s.mutex.Unlock()
//
//	s.Stats.TotalRequests++
//	s.Stats.TotalDuration += duration
//	s.Stats.TotalBytes += bytes
//	s.Stats.StatusCodes[status]++
//
//	if duration < s.Stats.MinDuration || s.Stats.MinDuration == 0 {
//		s.Stats.MinDuration = duration
//	}
//	if duration > s.Stats.MaxDuration {
//		s.Stats.MaxDuration = duration
//	}
//}
//
//func (s *Server) GetAndResetStats() ServerStats {
//	s.mutex.Lock()
//	defer s.mutex.Unlock()
//
//	if s.Stats.TotalRequests > 0 {
//		s.Stats.AvgDuration = time.Duration(int64(s.Stats.TotalDuration) / int64(s.Stats.TotalRequests))
//	}
//
//	snapshot := *s.Stats
//
//	*s.Stats = ServerStats{
//		StatusCodes: make(map[int]int),
//	}
//
//	return snapshot
//}
//
//func (s *Server) startStatsLogger(interval time.Duration) {
//	ticker := time.NewTicker(interval)
//	defer ticker.Stop()
//
//	for {
//		select {
//		case <-ticker.C:
//			stats := s.GetAndResetStats()
//			s.Logger.Info().
//				Interface("Statistics", stats).
//				Msg("Request statistics")
//		case <-s.done:
//			return
//		}
//	}
//}

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

type requestTask struct {
	body      []byte
	headers   http.Header
	replyChan chan responseResult
}

type responseResult struct {
	statusCode int
	body       []byte
	err        error
}

func (s *Server) HandleSend(w http.ResponseWriter, r *http.Request) {
	s.RequestWG.Add(1)
	defer s.RequestWG.Done()

	for _, name := range RequiredHeaders {
		if name == "x-esb-ver-id" || name == "x-esb-ver-no" {
			continue
		}
		if name == "x-esb-key" && !s.authenticateRequests {
			continue
		}
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
	if s.authenticateRequests && !isAuthenticated(esbKey) {
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.Logger.Error().
			Err(err).
			Int("status", http.StatusInternalServerError).
			Msg("Error reading request body")
		return
	}
	defer r.Body.Close()

	reply := make(chan responseResult, 1)
	task := &requestTask{
		headers:   r.Header,
		body:      body,
		replyChan: reply,
	}

	s.taskQueue <- task
	resp := <-reply

	if resp.err != nil {
		s.Logger.Error().Int("status", http.StatusInternalServerError).Msg(resp.err.Error())
		http.Error(w, resp.err.Error(), http.StatusInternalServerError)
		return
	}

	s.Logger.Info().
		Int("status", resp.statusCode).
		Interface("headers", r.Header).
		Int64("message_size", r.ContentLength).
		Str("body", string(resp.body)).
		Msg("Get response body")

	w.WriteHeader(resp.statusCode)
	w.Write(resp.body)
}

func (s *Server) Run() error {
	//http.HandleFunc("/send/", s.RequestStatsMiddleware(s.HandleSend))
	//http.HandleFunc("/send", s.RequestStatsMiddleware(s.HandleSend))
	//http.HandleFunc("/msg/", s.RequestStatsMiddleware(s.HandleSend))
	//http.HandleFunc("/msg", s.RequestStatsMiddleware(s.HandleSend))

	http.HandleFunc("/send/", s.HandleSend)
	http.HandleFunc("/send", s.HandleSend)
	http.HandleFunc("/msg/", s.HandleSend)
	http.HandleFunc("/msg", s.HandleSend)

	s.Logger.Info().
		Int("port", s.Port).
		Msg("Starting server")

	//go s.startStatsLogger(5 * time.Second)

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

func getAuthEnvVar() bool {
	env := os.Getenv("AUTHENTICATE_REQUESTS")
	if env == "" {
		return false
	}
	authDefaultValue, err := strconv.ParseBool(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parse environment variable: %v\n", err)
		os.Exit(1)
	}
	return authDefaultValue
}

func (s *Server) workerLoop(sess *HttpSession) {
	for task := range s.taskQueue {
		responseBody := string(task.body)
		resp, err := sess.TrySend(http.MethodPost, urlMsg, responseBody, task.headers)
		result := responseResult{}

		if err == nil {
			responceText, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 { //&& s.useSession {
				if (strings.Contains(string(responceText), "Ошибка работы сеанса")) ||
					(strings.Contains(string(responceText), "Session error")) {
					sess.ibSession = ""
					sess.useSession = true
					resp, err = sess.TrySend(http.MethodPost, urlMsg, responseBody, task.headers)
					if err == nil {
						responceText, _ = io.ReadAll(resp.Body)
					}
				}
			}
			result.statusCode = resp.StatusCode
			result.body, _ = io.ReadAll(resp.Body)
			resp.Body.Close()

		}

		result.err = err
		task.replyChan <- result
	}
}
func NewServer(port int, logFile string, authenticate bool, numWorkers int) (*Server, error) {
	logger, err := NewLogger(logFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	s := &Server{
		Port:    port,
		Logger:  logger,
		LogFile: logFile,
		//Stats: &ServerStats{
		//	StatusCodes: make(map[int]int),
		//},
		done:                 make(chan struct{}),
		taskQueue:            make(chan *requestTask, numWorkers*2),
		authenticateRequests: authenticate,
	}

	for i := 0; i < numWorkers; i++ {
		go s.workerLoop(NewSession(urlInfo, "esb", "esb", false))
	}

	return s, nil
}

const urlMsg = "http://localhost/msg" //http://10.0.0.240/ta_erp/hs/esb
const urlInfo = "http://10.0.0.240/ta_erp/hs/esb/info/"

func main() {
	port := flag.Int("port", 8080, "Server port")
	logFile := flag.String("log", "server.json", "Path to log file")
	authenticate := flag.Bool("auth", getAuthEnvVar(), "Authenticate HTTP requests")
	flag.Parse()

	server, err := NewServer(*port, *logFile, *authenticate, 1)
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
