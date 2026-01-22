// Package metrics collects and exports scraper metrics for Grafana.
package metrics

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

// Collector tracks metrics during a scrape run.
type Collector struct {
	mu sync.Mutex

	// Per-hall metrics
	hallSuccess  map[string]bool
	hallItems    map[string]int
	hallDuration map[string]time.Duration
	hallErrors   map[string][]string

	// Overall metrics
	totalItems            int
	totalNutrition        int
	itemsMissingNutrition int
	totalErrors           int
	daysScraped           int
	startTime             time.Time
	endTime               time.Time
	runSuccess            bool
}

// New creates a new metrics collector.
func New() *Collector {
	return &Collector{
		hallSuccess:  make(map[string]bool),
		hallItems:    make(map[string]int),
		hallDuration: make(map[string]time.Duration),
		hallErrors:   make(map[string][]string),
		startTime:    time.Now(),
		runSuccess:   true,
	}
}

// RecordHall records metrics for a single dining hall.
func (c *Collector) RecordHall(hall string, items int, duration time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hallItems[hall] = items
	c.hallDuration[hall] = duration

	if err != nil {
		c.hallSuccess[hall] = false
		c.hallErrors[hall] = append(c.hallErrors[hall], err.Error())
		c.runSuccess = false
	} else {
		c.hallSuccess[hall] = true
	}
}

// RecordHallError records an error for a hall (can be called multiple times).
func (c *Collector) RecordHallError(hall string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hallSuccess[hall] = false
	c.hallErrors[hall] = append(c.hallErrors[hall], err.Error())
	c.runSuccess = false
}

// SetTotals sets the overall run totals.
func (c *Collector) SetTotals(items, nutrition, missingNutrition, errors, days int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalItems = items
	c.totalNutrition = nutrition
	c.itemsMissingNutrition = missingNutrition
	c.totalErrors = errors
	c.daysScraped = days
}

// Finish marks the run as complete.
func (c *Collector) Finish(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.endTime = time.Now()
	c.runSuccess = success
}

// ToPrometheus formats metrics in Prometheus text exposition format (for debugging/file output).
func (c *Collector) ToPrometheus() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var buf bytes.Buffer

	writeMetric := func(name, help, metricType string) {
		buf.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		buf.WriteString(fmt.Sprintf("# TYPE %s %s\n", name, metricType))
	}

	writeMetric("feedme_scraper_hall_success", "Whether the hall scrape succeeded (1=success, 0=failure)", "gauge")
	for _, hall := range c.sortedHalls() {
		success := 0
		if c.hallSuccess[hall] {
			success = 1
		}
		buf.WriteString(fmt.Sprintf("feedme_scraper_hall_success{hall=%q} %d\n", hall, success))
	}

	writeMetric("feedme_scraper_hall_items", "Number of menu items scraped from this hall", "gauge")
	for _, hall := range c.sortedHalls() {
		buf.WriteString(fmt.Sprintf("feedme_scraper_hall_items{hall=%q} %d\n", hall, c.hallItems[hall]))
	}

	writeMetric("feedme_scraper_hall_duration_seconds", "Time to scrape this hall in seconds", "gauge")
	for _, hall := range c.sortedHalls() {
		buf.WriteString(fmt.Sprintf("feedme_scraper_hall_duration_seconds{hall=%q} %.3f\n", hall, c.hallDuration[hall].Seconds()))
	}

	writeMetric("feedme_scraper_run_success", "Whether the entire scrape run succeeded (1=success, 0=failure)", "gauge")
	success := 0
	if c.runSuccess {
		success = 1
	}
	buf.WriteString(fmt.Sprintf("feedme_scraper_run_success %d\n", success))

	writeMetric("feedme_scraper_total_items", "Total menu items scraped across all halls", "gauge")
	buf.WriteString(fmt.Sprintf("feedme_scraper_total_items %d\n", c.totalItems))

	writeMetric("feedme_scraper_total_nutrition", "Total nutrition records fetched", "gauge")
	buf.WriteString(fmt.Sprintf("feedme_scraper_total_nutrition %d\n", c.totalNutrition))

	writeMetric("feedme_scraper_items_missing_nutrition", "Number of items without nutrition data", "gauge")
	buf.WriteString(fmt.Sprintf("feedme_scraper_items_missing_nutrition %d\n", c.itemsMissingNutrition))

	writeMetric("feedme_scraper_errors_total", "Total number of errors during scrape", "gauge")
	buf.WriteString(fmt.Sprintf("feedme_scraper_errors_total %d\n", c.totalErrors))

	writeMetric("feedme_scraper_days_scraped", "Number of days scraped in this run", "gauge")
	buf.WriteString(fmt.Sprintf("feedme_scraper_days_scraped %d\n", c.daysScraped))

	writeMetric("feedme_scraper_duration_seconds", "Total scrape duration in seconds", "gauge")
	duration := c.endTime.Sub(c.startTime)
	if c.endTime.IsZero() {
		duration = time.Since(c.startTime)
	}
	buf.WriteString(fmt.Sprintf("feedme_scraper_duration_seconds %.3f\n", duration.Seconds()))

	writeMetric("feedme_scraper_last_success_timestamp", "Unix timestamp of last successful run", "gauge")
	if c.runSuccess && !c.endTime.IsZero() {
		buf.WriteString(fmt.Sprintf("feedme_scraper_last_success_timestamp %d\n", c.endTime.Unix()))
	}

	return buf.String()
}

// Push sends metrics to Grafana Cloud using Prometheus Remote Write protocol.
func (c *Collector) Push(url, username, apiKey string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UnixMilli()
	var timeseries []prompb.TimeSeries

	// Helper to create a timeseries
	addMetric := func(name string, value float64, extraLabels ...prompb.Label) {
		labels := []prompb.Label{
			{Name: "__name__", Value: name},
			{Name: "job", Value: "feedme_scraper"},
		}
		labels = append(labels, extraLabels...)
		timeseries = append(timeseries, prompb.TimeSeries{
			Labels:  labels,
			Samples: []prompb.Sample{{Value: value, Timestamp: now}},
		})
	}

	// Per-hall metrics
	for hall, success := range c.hallSuccess {
		hallLabel := prompb.Label{Name: "hall", Value: hall}
		if success {
			addMetric("feedme_scraper_hall_success", 1, hallLabel)
		} else {
			addMetric("feedme_scraper_hall_success", 0, hallLabel)
		}
	}
	for hall, items := range c.hallItems {
		hallLabel := prompb.Label{Name: "hall", Value: hall}
		addMetric("feedme_scraper_hall_items", float64(items), hallLabel)
	}
	for hall, dur := range c.hallDuration {
		hallLabel := prompb.Label{Name: "hall", Value: hall}
		addMetric("feedme_scraper_hall_duration_seconds", dur.Seconds(), hallLabel)
	}

	// Overall metrics
	if c.runSuccess {
		addMetric("feedme_scraper_run_success", 1)
	} else {
		addMetric("feedme_scraper_run_success", 0)
	}
	addMetric("feedme_scraper_total_items", float64(c.totalItems))
	addMetric("feedme_scraper_total_nutrition", float64(c.totalNutrition))
	addMetric("feedme_scraper_items_missing_nutrition", float64(c.itemsMissingNutrition))
	addMetric("feedme_scraper_errors_total", float64(c.totalErrors))
	addMetric("feedme_scraper_days_scraped", float64(c.daysScraped))

	duration := c.endTime.Sub(c.startTime)
	if c.endTime.IsZero() {
		duration = time.Since(c.startTime)
	}
	addMetric("feedme_scraper_duration_seconds", duration.Seconds())

	if c.runSuccess && !c.endTime.IsZero() {
		addMetric("feedme_scraper_last_success_timestamp", float64(c.endTime.Unix()))
	}

	// Create WriteRequest
	writeReq := &prompb.WriteRequest{Timeseries: timeseries}

	// Marshal to protobuf
	data, err := proto.Marshal(writeReq)
	if err != nil {
		return fmt.Errorf("marshal protobuf: %w", err)
	}

	// Compress with snappy
	compressed := snappy.Encode(nil, data)

	// Send HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.SetBasicAuth(username, apiKey)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("push request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("push failed: status %d", resp.StatusCode)
	}

	return nil
}

// sortedHalls returns hall names in sorted order for consistent output.
func (c *Collector) sortedHalls() []string {
	halls := make([]string, 0, len(c.hallItems))
	for hall := range c.hallItems {
		halls = append(halls, hall)
	}
	sort.Strings(halls)
	return halls
}
