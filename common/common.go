package common

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Fields map[string]interface{}
type LogLevel int

var Level = [...]string{
	"Info",
	"Warn",
	"Error",
}

var RequiredHeaders = [...]string{
	"x-esb-src",
	"x-esb-data-type",
	"x-esb-ver-id",
	"x-esb-ver-no",
	"x-esb-key",
}

const (
	Info LogLevel = iota
	Warn
	Error
)

type CallerInfo struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type Message struct {
	ID        string    `json:"id"`
	Payload   string    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

type Logger struct {
	logger *log.Logger
	file   *os.File
	buffer *bufio.Writer
	done   chan struct{}
	mutex  sync.Mutex
}

func (l LogLevel) String() string {
	return Level[l]
}

func (l LogLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (l LogLevel) String() string {
	return Level[l]
}

func (l LogLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func NewLogger(logFilePath string) (*Logger, error) {
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	buffered := bufio.NewWriterSize(file, 1024*1024)

	logger := &Logger{
		buffer: buffered,
		logger: log.New(buffered, "", 0),
		file:   file,
		done:   make(chan struct{}),
	}

	go flushTicker(logger, logger.done, 1*time.Second)

	return logger, nil
}

func flushTicker(l *Logger, done chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := l.Flush(); err != nil {
				fmt.Printf("failed to flush buffer: %v\n", err)
			}
		case <-done:
			return
		}
	}
}

func (l *Logger) Flush() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.buffer.Flush()
}

func getCaller(skip int) CallerInfo {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return CallerInfo{"unknown", -1}
	}

	fileName := filepath.Base(file)

	return CallerInfo{fileName, line}
}

func getCaller(skip int) CallerInfo {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return CallerInfo{"unknown", -1}
	}

	fileName := filepath.Base(file)

	return CallerInfo{fileName, line}
}

func (l *Logger) log(level LogLevel, message string, fields Fields) {
	caller := getCaller(3)

	entry := &struct {
		Level      LogLevel   `json:"level"`
		Timestamp  time.Time  `json:"timestamp"`
		Message    string     `json:"message"`
		CallerInfo CallerInfo `json:"caller"`
		Fields     `json:"log,omitempty"`
	}{
		Level:      level,
		Timestamp:  time.Now(),
		Message:    message,
		CallerInfo: caller,
		Fields:     fields,
	}

	jsonEntry, err := json.Marshal(entry)
	if err != nil {
		l.logger.Printf("Error marshaling log entry: %v", err)
		return
	}
	l.mutex.Lock()
	l.logger.Println(string(jsonEntry))
	l.mutex.Unlock()
}

func (l *Logger) Info(message string, fields Fields) {
	l.log(Info, message, fields)
}

func (l *Logger) Warn(message string, fields Fields) {
	l.log(Warn, message, fields)
}

func (l *Logger) Error(message string, fields Fields) {
	l.log(Error, message, fields)
}

func (l *Logger) Close() error {
	close(l.done)
	err := l.Flush()
	if err != nil {
		return err
	}
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
