package main

import (
	"net/http"
	"time"
)

// In-container probe; loopback is explicitly trusted by the local stack.
func healthy() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", "http://127.0.0.1:9090/health/ready", nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == 200
}
