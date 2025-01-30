package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"golang.org/x/time/rate"
)

const (
	totalRequests = 15
)

var (
	targetUrl     string
	successCount  = 0
	failureCount  = 0
	mu            sync.Mutex
	wg            sync.WaitGroup
	responseTimes []time.Duration
	myClient      = &http.Client{Timeout: 3000 * time.Second}
)

var limiter = rate.NewLimiter(rate.Every(time.Second/100), 1)

func fetch(i int) {
	if err := limiter.Wait(context.Background()); err != nil {
		fmt.Println("Error waiting for rate limiter:", err)
		return
	}

	defer wg.Done()

	var resp *http.Response
	var err error
	var elapsed time.Duration

	for attempts := 0; attempts < 3; attempts++ {
		start := time.Now()
		var req *http.Request
		req, err = http.NewRequest("GET", targetUrl, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		// Check if the URL contains "/api" and add headers and data
		if strings.Contains(targetUrl, "/api") {
			// Load headers from a JSON file
			headers, err := loadHeaders("headers.json")
			if err != nil {
				fmt.Println(err)
				return
			}

			for key, value := range headers {
				req.Header.Add(key, value)
			}

			// Add the data payload
			req.Method = "POST"
			req.Header.Set("Content-Type", "application/json")
			req.Body = io.NopCloser(strings.NewReader(`{"action":"get_stats"}`))
		}

		resp, err = myClient.Do(req)
		elapsed = time.Since(start)

		if err != nil {
			fmt.Println(err)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// If it's a timeout error, retry the request
				continue
			} else {
				// If it's another kind of error, don't retry
				return
			}
		} else {
			// If there's no error, break the loop
			break
		}
	}

	if resp != nil {
		defer resp.Body.Close()

		if resp.StatusCode == 400 {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				fmt.Println("Error reading response body:", err)
				return
			}
			fmt.Println("Response body:", string(bodyBytes))
		}
	}

	mu.Lock()
	responseTimes = append(responseTimes, elapsed)
	if resp != nil && resp.StatusCode == 200 {
		successCount++
	} else {
		failureCount++
	}
	mu.Unlock()
}

func loadHeaders(filename string) (map[string]string, error) {
	var headers map[string]string

	bytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(bytes, &headers)
	if err != nil {
		return nil, err
	}

	return headers, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run stress.go <url>")
		os.Exit(1)
	}

	targetUrl = os.Args[1]

	start := time.Now()

	wg.Add(totalRequests)

	for i := 0; i < totalRequests; i++ {
		go fetch(i)
	}
	wg.Wait()

	totalElapsed := time.Since(start)

	var totalResponseTime time.Duration
	for _, t := range responseTimes {
		totalResponseTime += t
	}

	averageResponseTime := totalResponseTime / time.Duration(len(responseTimes))
	averageRequestRate := float64(totalRequests) / totalElapsed.Seconds()
	successRate := float64(successCount) / float64(totalRequests) * 100

	parsedUrl, err := url.Parse(targetUrl)
	if err != nil {
		fmt.Println("Invalid URL")
		os.Exit(1)
	}

	osPrefix := ""
	if strings.Contains(strings.ToLower(parsedUrl.Hostname()), "linux") {
		osPrefix = "Linux"
	} else {
		osPrefix = "Windows"
	}

	fmt.Printf("Total: %d | Success: %d | Failure: %d | Rate: %.2f%%\n", totalRequests, successCount, failureCount, successRate)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight|tabwriter.Debug)
	fmt.Fprintln(w, "Metric\tValue")
	fmt.Fprintf(w, "%s\t%s\n", osPrefix, parsedUrl.Hostname())
	fmt.Fprintf(w, "Total execution time\t%.2f sec\n", math.Round(totalElapsed.Seconds()*100)/100)
	fmt.Fprintf(w, "Average response time\t%.2f sec\n", math.Round(averageResponseTime.Seconds()*100)/100)
	fmt.Fprintf(w, "Average request rate\t%.2f requests/second\n", averageRequestRate)
	w.Flush()
}
