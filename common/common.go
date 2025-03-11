package common

import (
	"fmt"
	"go.elastic.co/ecszerolog"
	"os"
	"time"

	"github.com/rs/zerolog"
)

type Fields map[string]interface{}

var RequiredHeaders = [...]string{
	"x-esb-src",
	"x-esb-data-type",
	"x-esb-ver-id",
	"x-esb-ver-no",
	"x-esb-key",
}

var DataTypes = [...]string{"json", "xml", "html", "form-data", "binary"}

var EsbKeys = [...]string{"NiuqR6nZ8ZlSpz3d5rIyj1NqeTM", "kJ8gFC0sBKuasutvyg2yLVUhyz7", "o5usHtlb5KoK7KUjqSscynmcHWE"}

type Message struct {
	ID        string    `json:"id"`
	Payload   string    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

type Logger struct {
	zerolog.Logger
	file *os.File
}

func NewLogger(logFilePath string) (*Logger, error) {
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	logger := &Logger{
		Logger: ecszerolog.New(file).With().Caller().Logger(),
		file:   file,
	}

	return logger, nil
}

func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
