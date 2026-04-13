// Package fetch provides an HTTP client with rate limiting and caching.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Client is a rate-limited HTTP client for fetching web pages.
type Client struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	cache      *Cache
	userAgent  string
	mu         sync.Mutex

	// Stats
	totalRequests int
	cacheHits     int
	cacheMisses   int
	errors        int
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithRateLimit sets the rate limit (requests per second).
func WithRateLimit(rps float64) ClientOption {
	return func(c *Client) {
		c.limiter = rate.NewLimiter(rate.Limit(rps), 1)
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithCache enables caching with the given cache instance.
func WithCache(cache *Cache) ClientOption {
	return func(c *Client) {
		c.cache = cache
	}
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) ClientOption {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// New creates a new rate-limited HTTP client.
func New(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		limiter:   rate.NewLimiter(1, 1), // Default: 1 request per second
		userAgent: "FeedMe-Scraper/1.0 (UCLA Dining Menu Aggregator)",
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// FetchResult contains the result of a fetch operation.
type FetchResult struct {
	URL       string
	Body      string
	FromCache bool
	Error     error
}

// Get fetches a URL, respecting rate limits and using cache if available.
func (c *Client) Get(ctx context.Context, url string) (string, bool, error) {
	c.mu.Lock()
	c.totalRequests++
	c.mu.Unlock()

	// Check cache first
	if c.cache != nil {
		if body, ok := c.cache.Get(url); ok {
			c.mu.Lock()
			c.cacheHits++
			c.mu.Unlock()
			return body, true, nil
		}
	}

	c.mu.Lock()
	c.cacheMisses++
	c.mu.Unlock()

	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		c.mu.Lock()
		c.errors++
		c.mu.Unlock()
		return "", false, fmt.Errorf("rate limiter: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.mu.Lock()
		c.errors++
		c.mu.Unlock()
		return "", false, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.mu.Lock()
		c.errors++
		c.mu.Unlock()
		return "", false, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.mu.Lock()
		c.errors++
		c.mu.Unlock()
		return "", false, fmt.Errorf("http status %d for %s", resp.StatusCode, url)
	}

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.mu.Lock()
		c.errors++
		c.mu.Unlock()
		return "", false, fmt.Errorf("read body: %w", err)
	}

	bodyStr := string(body)

	// Store in cache
	if c.cache != nil {
		c.cache.Set(url, bodyStr)
	}

	return bodyStr, false, nil
}

// HTTPClient returns the underlying *http.Client for use by callers
// that need to make additional HTTP requests with the same timeout settings.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// Stats returns fetch statistics.
func (c *Client) Stats() (total, hits, misses, errs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalRequests, c.cacheHits, c.cacheMisses, c.errors
}

// ResetStats resets the fetch statistics.
func (c *Client) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalRequests = 0
	c.cacheHits = 0
	c.cacheMisses = 0
	c.errors = 0
}
