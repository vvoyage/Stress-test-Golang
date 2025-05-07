package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	. "stress/common"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	Host                  string
	Port                  string
	Threads               int
	MessagesCount         int
	LogFile               string
	MinPayload            int
	MaxPayload            int
	BrokenHeadersPercent  int
	InvalidHeadersPercent int
}

type Client struct {
	Config  *Config
	Headers *http.Header
	Logger  *Logger
	Stats   *Statistics
}

type Statistics struct {
	TotalRequests      int
	SuccessfulRequests int
	FailedRequests     int
	TotalDuration      time.Duration
	MinDuration        time.Duration
	MaxDuration        time.Duration
	mutex              sync.Mutex
}

func NewStatistics() *Statistics {
	return &Statistics{
		MinDuration: time.Hour,
	}
}

func (s *Statistics) RecordRequest(success bool, duration time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.TotalRequests++
	s.TotalDuration += duration

	if success {
		s.SuccessfulRequests++
	} else {
		s.FailedRequests++
	}

	if duration < s.MinDuration {
		s.MinDuration = duration
	} else if duration > s.MaxDuration {
		s.MaxDuration = duration
	}
}

func (s *Statistics) GetSummary() Fields {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	successRate := 0.0
	if s.TotalRequests > 0 {
		successRate = float64(s.SuccessfulRequests) / float64(s.TotalRequests) * 100
	}

	avgDuration := time.Duration(0)
	if s.TotalRequests > 0 {
		avgDuration = time.Duration(int64(s.TotalDuration) / int64(s.TotalRequests))
	}

	return Fields{
		"TotalRequests":      s.TotalRequests,
		"SuccessfulRequests": s.SuccessfulRequests,
		"FailedRequests":     s.FailedRequests,
		"SuccessRate":        fmt.Sprintf("%.2f%%", successRate),
		"AverageDuration":    avgDuration,
		"MinDuration":        s.MinDuration,
		"MaxDuration":        s.MaxDuration,
	}
}

func NewClient(config *Config, headers *http.Header) (*Client, error) {
	logger, err := NewLogger(config.LogFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return &Client{
		Config:  config,
		Headers: headers,
		Logger:  logger,
		Stats:   NewStatistics(),
	}, nil
}

func getRandomHeaders(baseHeaders *http.Header, threadID int, brokenHeadersPercent int, invalidHeadersPercent int) http.Header {
	headers := http.Header{}

	for _, header := range RequiredHeaders {
		if rand.Intn(100) < brokenHeadersPercent {
			continue
		}

		value := ""
		switch header {
		case "x-esb-src":
			value = "sys:erp" //fmt.Sprint(threadID)
		case "x-esb-data-type":
			value = DataTypes[rand.Intn(len(DataTypes))]
		case "x-esb-ver-id":
			value = uuid.New().String()
		case "x-esb-key":
			value = EsbKeys[rand.Intn(len(EsbKeys))]
		case "x-esb-ver-no":
			t := time.Now()
			value = t.Format("20060102T150405")
		default:
			value = baseHeaders.Get(header)
		}

		if rand.Intn(100) < invalidHeadersPercent {
			value = "invalid-value"
		}

		headers.Set(header, value)
	}

	return headers
}

var letters = [62]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}

func randomPayload(size int) string {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = letters[rand.Intn(len(letters))]
	}
	return string(payload)
}

func (c *Client) SendMessage(ctx context.Context, httpClient *http.Client, threadID int, messageNumber int, randomHeaders http.Header) (time.Duration, int, error) {
	messageID := strconv.Itoa(threadID) + "-" + strconv.Itoa(messageNumber)
	size := rand.Intn(c.Config.MaxPayload-c.Config.MinPayload+1) + c.Config.MinPayload
	message := &Message{
		ID:        messageID,
		Payload:   randomPayload(size),
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return 0, 0, fmt.Errorf("error marshaling message: %w", err)
	}

	url := fmt.Sprintf("http://%s:%s/msg", c.Config.Host, c.Config.Port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return 0, 0, fmt.Errorf("error creating request: %w", err)
	}

	req.Header = randomHeaders

	if len(randomHeaders) < len(RequiredHeaders) {
		c.Logger.Warn().
			Int("thread_id", threadID).
			Str("message_id", messageID).
			Msg("Missing headers in request")
	}

	for header, value := range randomHeaders {
		if value[0] == "invalid-value" {
			c.Logger.Warn().
				Int("thread_id", threadID).
				Str("message_id", messageID).
				Str("header", header).
				Msg("Invalid header value in request")
		}
	}

	c.Logger.Info().
		Int("thread_id", threadID).
		Str("message_id", messageID).
		Int("payload_size", size).
		Interface("selectedHeaders", randomHeaders).
		Msg("Sending message")

	startTime := time.Now()
	resp, err := httpClient.Do(req)
	duration := time.Since(startTime)
	if err != nil {
		c.Stats.RecordRequest(false, duration)
		return duration, 0, err
	}

	defer resp.Body.Close()

	success := resp.StatusCode == 200
	c.Stats.RecordRequest(success, duration)
	b, err := io.ReadAll(resp.Body)

	c.Logger.Info().
		Int("thread_id", threadID).
		Str("message_id", messageID).
		Dur("duration", duration).
		Int("status", resp.StatusCode).
		Msg(string(b))

	return duration, resp.StatusCode, nil
}

func (c *Client) RunThread(ctx context.Context, threadID int, wg *sync.WaitGroup) {
	defer wg.Done()

	httpClient := &http.Client{
		Timeout:   1 * time.Second,
		Transport: &http.Transport{},
	}
	defer httpClient.CloseIdleConnections()

	c.Logger.Info().
		Int("thread_id", threadID).
		Msg("Starting thread")

	randomHeaders := getRandomHeaders(c.Headers, threadID, c.Config.BrokenHeadersPercent, c.Config.InvalidHeadersPercent)

	for i := 1; i <= c.Config.MessagesCount; i++ {
		select {
		case <-ctx.Done():
			c.Logger.Warn().
				Int("thread_id", threadID).
				Msg("Thread interrupted")
			return
		default:
			_, _, err := c.SendMessage(ctx, httpClient, threadID, i, randomHeaders)
			if err != nil {
				c.Logger.Error().
					Int("thread_id", threadID).
					Err(err).
					Msg("Error in thread")
			}
		}
	}

	c.Logger.Info().
		Int("thread_id", threadID).
		Msg("Thread completed")
}

func (c *Client) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Logger.Info().
		Int("threads", c.Config.Threads).
		Int("messages_per_thread", c.Config.MessagesCount).
		Msg("Starting client")

	var wg sync.WaitGroup
	wg.Add(c.Config.Threads)

	startTime := time.Now()

	for i := 1; i <= c.Config.Threads; i++ {
		go c.RunThread(ctx, i, &wg)
	}

	wg.Wait()

	duration := time.Since(startTime)
	totalMessages := c.Config.Threads * c.Config.MessagesCount

	stats := c.Stats.GetSummary()

	c.Logger.Info().
		Int("total_messages", totalMessages).
		Dur("duration", duration).
		Float64("messages_per_second", float64(totalMessages)/duration.Seconds()).
		Msg("Client completed")

	c.Logger.Info().
		Fields(stats).
		Msg("Response Statistics")

	fmt.Printf("\n--- Response Statistics ---\n")
	fmt.Printf("Total Requests:      %d\n", stats["TotalRequests"])
	fmt.Printf("Successful Requests: %d\n", stats["SuccessfulRequests"])
	fmt.Printf("Failed Requests:     %d\n", stats["FailedRequests"])
	fmt.Printf("Success Rate:        %s\n", stats["SuccessRate"])
	fmt.Printf("Average Response:    %v\n", stats["AverageDuration"])
	fmt.Printf("Minimum Response:    %v\n", stats["MinDuration"])
	fmt.Printf("Maximum Response:    %v\n", stats["MaxDuration"])
	fmt.Printf("-------------------------\n")

	if err := c.Logger.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing log file: %v\n", err)
	}
}

func main() {
	host := flag.String("host", os.Getenv("SERVICE_HOST"), "Service host")
	port := flag.String("port", os.Getenv("SERVICE_PORT"), "Service port")
	threads := flag.Int("threads", getEnvInt("THREADS", 6), "Number of threads")
	messages := flag.Int("messages", getEnvInt("MESSAGES", 100), "Number of messages per thread")
	minPayload := flag.Int("min-payload", getEnvInt("MIN_PAYLOAD", 10), "Minimum payload size in bytes")
	maxPayload := flag.Int("max-payload", getEnvInt("MAX_PAYLOAD", 1024), "Maximum payload size in bytes")
	logFile := flag.String("log", "client.json", "Path to log file")
	brokenHeadersPercent := flag.Int("broken-headers-percent", getEnvInt("BROKEN_HEADERS_PERCENT", 10), "Percentage of requests with missing headers")
	invalidHeadersPercent := flag.Int("invalid-headers-percent", getEnvInt("INVALID_HEADERS_PERCENT", 10), "Percentage of requests with invalid header values")

	flag.Parse()

	if *host == "" {
		*host = "localhost"
	}
	if *port == "" {
		*port = "8080"
	}

	headers := http.Header{}

	config := &Config{
		Host:                  *host,
		Port:                  *port,
		Threads:               *threads,
		MessagesCount:         *messages,
		LogFile:               *logFile,
		MinPayload:            *minPayload,
		MaxPayload:            *maxPayload,
		BrokenHeadersPercent:  *brokenHeadersPercent,
		InvalidHeadersPercent: *invalidHeadersPercent,
	}

	client, err := NewClient(config, &headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating client: %v\n", err)
		os.Exit(1)
	}

	client.Run()
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
