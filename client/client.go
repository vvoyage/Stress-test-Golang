package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	Host          string
	Port          string
	Threads       int
	MessagesCount int
	LogFile       string
	MinPayload    int
	MaxPayload    int
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
		successRate = float64(s.SuccessfulRequests) / float64(s.TotalRequests*100)
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

func getRandomHeaders(baseHeaders *http.Header, threadID int) http.Header {
	headers := http.Header{}

	for _, header := range RequiredHeaders {
		value := ""
		switch header {
		case "x-esb-src":
			value = fmt.Sprint(threadID)
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

	url := fmt.Sprintf("http://%s:%s/send/", c.Config.Host, c.Config.Port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return 0, 0, fmt.Errorf("error creating request: %w", err)
	}

	req.Header = randomHeaders

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

	c.Logger.Info().
		Int("thread_id", threadID).
		Str("message_id", messageID).
		Dur("duration", duration).
		Int("status", resp.StatusCode).
		Msg("Message sent")

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

	randomHeaders := getRandomHeaders(c.Headers, threadID)

	randomHeaders := getRandomHeaders(c.Headers, threadID)

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
	threads := flag.Int("threads", 12, "Number of threads")
	messages := flag.Int("messages", 30, "Number of messages per thread")
	minPayload := flag.Int("min-payload", 10, "Minimum payload size in bytes")
	maxPayload := flag.Int("max-payload", 1024, "Maximum payload size in bytes")
	logFile := flag.String("log", "client.log", "Path to log file")
	esbSrc := flag.String("esb-src", "client-app", "ESB source")
	esbDataType := flag.String("esb-data-type", "json", "ESB data type")
	esbVerID := flag.String("esb-ver-id", "v1", "ESB version ID")
	esbVerNo := flag.String("esb-ver-no", "1.0", "ESB version number")
	esbKey := flag.String("esb-key", "default-key", "ESB key")
	flag.Parse()

	if *host == "" {
		*host = "localhost"
	}
	if *port == "" {
		*port = "8080"
	}

	headers := http.Header{}
	headers.Set("x-esb-src", *esbSrc)
	headers.Set("x-esb-data-type", *esbDataType)
	headers.Set("x-esb-ver-id", *esbVerID)
	headers.Set("x-esb-ver-no", *esbVerNo)
	headers.Set("x-esb-key", *esbKey)

	config := &Config{
		Host:          *host,
		Port:          *port,
		Threads:       *threads,
		MessagesCount: *messages,
		LogFile:       *logFile,
		MinPayload:    *minPayload,
		MaxPayload:    *maxPayload,
	}

	client, err := NewClient(config, &headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating client: %v\n", err)
		os.Exit(1)
	}

	client.Run()
}
