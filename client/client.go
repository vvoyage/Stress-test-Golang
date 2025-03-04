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
)

type Config struct {
	Host          string
	Port          int
	Threads       int
	MessagesCount int
	LogFile       string
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

func getRandomHeaders(baseHeaders *http.Header) http.Header {
	headers := http.Header{}
	numHeaders := rand.Intn(len(RequiredHeaders)) + 1

	selectedIndexes := rand.Perm(len(RequiredHeaders))[:numHeaders]

	for _, idx := range selectedIndexes {
		key := RequiredHeaders[idx]
		headers.Set(key, baseHeaders.Get(key))
	}

	return headers
}

func (c *Client) SendMessage(ctx context.Context, httpClient *http.Client, threadID int, messageNumber int) (time.Duration, int, error) {
	messageID := strconv.Itoa(threadID) + "-" + strconv.Itoa(messageNumber)
	message := &Message{
		ID:        messageID,
		Payload:   messageID,
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return 0, 0, fmt.Errorf("error marshaling message: %w", err)
	}

	url := fmt.Sprintf("http://%s:%d/send/", c.Config.Host, c.Config.Port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return 0, 0, fmt.Errorf("error creating request: %w", err)
	}

	for _, name := range RequiredHeaders {
		req.Header.Set(name, c.Headers.Get(name))
	}

	randomHeaders := getRandomHeaders(c.Headers)

	req.Header = randomHeaders

	c.Logger.Info("Sending message", Fields{
		"thread_id":       threadID,
		"message_id":      messageID,
		"selectedHeaders": randomHeaders,
	})

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

	c.Logger.Info("Message sent", Fields{
		"thread_id":   threadID,
		"message_id":  messageID,
		"duration_ms": duration.Milliseconds(),
		"status":      resp.StatusCode,
	})

	return duration, resp.StatusCode, nil
}

func (c *Client) RunThread(ctx context.Context, threadID int, wg *sync.WaitGroup) {
	defer wg.Done()

	httpClient := &http.Client{
		Timeout:   1 * time.Second,
		Transport: &http.Transport{},
	}
	defer httpClient.CloseIdleConnections()

	c.Logger.Info("Thread started", Fields{
		"thread_id": threadID,
	})

	for i := 1; i <= c.Config.MessagesCount; i++ {
		select {
		case <-ctx.Done():
			c.Logger.Warn("Thread interrupted", Fields{
				"thread_id": threadID,
			})
			return
		default:
			_, _, err := c.SendMessage(ctx, httpClient, threadID, i)
			if err != nil {
				c.Logger.Error("Error in thread", Fields{
					"thread_id": threadID,
					"error":     err.Error(),
				})
			}
		}
	}

	c.Logger.Info("Thread completed", Fields{
		"thread_id": threadID,
	})
}

func (c *Client) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Logger.Info("Starting client", Fields{
		"threads":             c.Config.Threads,
		"messages_per_thread": c.Config.MessagesCount,
	})

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

	c.Logger.Info("Client completed", Fields{
		"total_messages":      totalMessages,
		"duration":            duration.String(),
		"messages_per_second": float64(totalMessages) / duration.Seconds(),
	})

	c.Logger.Info("Response Statistics", Fields{
		"success_rate":      stats["SuccessRate"],
		"avg_response_time": stats["AverageDuration"],
		"min_response_time": stats["MinDuration"],
		"max_response_time": stats["MaxDuration"],
	})

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
	host := flag.String("host", "192.168.0.25", "Service host")
	port := flag.Int("port", 8080, "Service port")
	threads := flag.Int("threads", 12, "Number of threads")
	messages := flag.Int("messages", 30, "Number of messages per thread")
	logFile := flag.String("log", "client.log", "Path to log file")
	esbSrc := flag.String("esb-src", "client-app", "ESB source")
	esbDataType := flag.String("esb-data-type", "json", "ESB data type")
	esbVerID := flag.String("esb-ver-id", "v1", "ESB version ID")
	esbVerNo := flag.String("esb-ver-no", "1.0", "ESB version number")
	esbKey := flag.String("esb-key", "default-key", "ESB key")
	flag.Parse()

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
	}

	client, err := NewClient(config, &headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating client: %v\n", err)
		os.Exit(1)
	}

	client.Run()
}
