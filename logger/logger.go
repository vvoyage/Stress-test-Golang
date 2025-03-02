package logger

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func New(filename string) (*Logger, error) {
	var file *os.File
	var err error

	file, err = os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &Logger{file: file}, nil
}

func (l *Logger) logLevel(level string, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000") // Исправлен формат времени
	entry := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.file.WriteString(entry)
}

func (l *Logger) Info(message string) {
	l.logLevel("INFO", message)
}

func (l *Logger) Error(message string) {
	l.logLevel("ERROR", message)
}

func (l *Logger) Close() {
	if l.file != os.Stdout {
		l.file.Close()
	}
}
