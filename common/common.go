package common

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

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

type Fields map[string]interface{}
type LogLevel int

const (
	Info LogLevel = iota
	Warn
	Error
)

func (l LogLevel) String() string {
	return Level[l]
}

func (l LogLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type Message struct {
	ID        string    `json:"id"`
	Payload   string    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

type Logger struct {
	logger *log.Logger
	file   *os.File
}

func NewLogger(logFilePath string) (*Logger, error) {
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return &Logger{
		logger: log.New(file, "", 0),
		file:   file,
	}, nil
}

func (l *Logger) log(level LogLevel, message string, fields Fields) {
	entry := struct {
		Level     LogLevel  `json:"level"`
		Timestamp time.Time `json:"timestamp"`
		Message   string    `json:"message"`
		Log       Fields    `json:"log,omitempty"`
	}{
		Level:     level,
		Timestamp: time.Now(),
		Message:   message,
		Log:       fields,
	}

	jsonEntry, err := json.Marshal(entry)
	if err != nil {
		l.logger.Printf("Error marshaling log entry: %v", err)
		return
	}

	l.logger.Println(string(jsonEntry))
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
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
