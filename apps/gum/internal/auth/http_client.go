package auth

import (
	"net/http"
	"time"
)

const defaultAuthHTTPTimeout = 30 * time.Second

func newDefaultAuthHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultAuthHTTPTimeout}
}
