package main

import (
	"flag"
	"net/http"
	. "stress/common"
)

type Dumper struct {
	Proxy  *http.Client
	Logger *Logger
}

func NewDumper(logFile *string) *Dumper {
	logger, _ := NewLogger(*logFile)
	ph := Dumper{
		Logger: logger,
	}
	return &ph
}

func (h *Dumper) DumpRequest(w http.ResponseWriter, r *http.Request) {
	h.Logger.Info().Interface("headers", r.Header).Msg("request received")
	w.WriteHeader(http.StatusOK)
}

func main() {
	logFile := flag.String("log", "proxy.json", "Path to log file")

	flag.Parse()

	dumper := NewDumper(logFile)

	http.HandleFunc("/msg", dumper.DumpRequest)

	err := http.ListenAndServe("localhost:80", nil)
	if err != nil {
		panic(err)
	}
}
