package main

import (
	"flag"
	"net/http"
	"net/http/httputil"
	"net/url"

	. "stress/common"
)

type ProxyHandler struct {
	Proxy  *httputil.ReverseProxy
	Logger *Logger
}

func NewProxyHandler(destUrl *url.URL, logFile *string) *ProxyHandler {
	logger, _ := NewLogger(*logFile)
	ph := ProxyHandler{
		Proxy:  httputil.NewSingleHostReverseProxy(destUrl),
		Logger: logger,
	}
	ph.Proxy.Transport = &ph
	return &ph
}

func (t *ProxyHandler) RoundTrip(request *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(request)
}

func (h *ProxyHandler) ProxyRequest(w http.ResponseWriter, r *http.Request) {
	h.Logger.Info().Interface("headers", r.Header).Msgf("> ProxyRequest, Client: %v, %v %v %v\n", r.RemoteAddr, r.Method, r.URL, r.Proto)
	h.Proxy.ServeHTTP(w, r)
}

func main() {
	svrAddr := flag.String("p", ":8900", "Proxy Server Address")
	destUrlStr := flag.String("d", "http://dispatch:8950", "destination url")
	logFile := flag.String("log", "proxy.json", "Path to log file")
	println("1123123123131")
	flag.Parse()

	destUrl, _ := url.Parse(*destUrlStr)
	proxyHandler := NewProxyHandler(destUrl, logFile)

	http.HandleFunc("/", proxyHandler.ProxyRequest)

	err := http.ListenAndServe(*svrAddr, nil)
	if err != nil {
		panic(err)
	}
}
