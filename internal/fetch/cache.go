package fetch

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheEntry represents a cached HTTP response.
type CacheEntry struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag"`
	LastModified string    `json:"last_modified"`
	ResponseText string    `json:"response_text"`
	CachedAt     time.Time `json:"cached_at"`
}

// Cache is a file-based HTTP response cache.
type Cache struct {
	dir string
	ttl time.Duration
}

// NewCache creates a new file-based cache.
func NewCache(dir string, ttl time.Duration) (*Cache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	return &Cache{
		dir: dir,
		ttl: ttl,
	}, nil
}

// hashURL creates an MD5 hash of the URL for the cache filename.
func hashURL(url string) string {
	h := md5.Sum([]byte(url))
	return hex.EncodeToString(h[:])
}

// filepath returns the cache file path for a URL.
func (c *Cache) filepath(url string) string {
	return filepath.Join(c.dir, hashURL(url)+".json")
}

// Get retrieves a cached response if it exists and is not expired.
func (c *Cache) Get(url string) (string, bool) {
	path := c.filepath(url)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", false
	}

	// Check TTL
	if time.Since(entry.CachedAt) > c.ttl {
		return "", false
	}

	// Verify URL matches (collision check)
	if entry.URL != url {
		return "", false
	}

	return entry.ResponseText, true
}

// Set stores a response in the cache.
func (c *Cache) Set(url, body string) error {
	entry := CacheEntry{
		URL:          url,
		ResponseText: body,
		CachedAt:     time.Now(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal cache entry: %w", err)
	}

	path := c.filepath(url)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	return nil
}

// Clear removes all cached entries.
func (c *Cache) Clear() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cache dir: %w", err)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			path := filepath.Join(c.dir, entry.Name())
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove cache file %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// Count returns the number of cached entries.
func (c *Cache) Count() (int, error) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}

	return count, nil
}

// LoadFromExisting loads cache entries from an existing cache directory
// that may have a different format (like the user's .cache folder).
// This supports the format: {"url": "...", "response_text": "..."}
func (c *Cache) LoadFromExisting(existingDir string) (int, error) {
	entries, err := os.ReadDir(existingDir)
	if err != nil {
		return 0, fmt.Errorf("read existing cache dir: %w", err)
	}

	loaded := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(existingDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Try to parse as simple format
		var simple struct {
			URL          string `json:"url"`
			ResponseText string `json:"response_text"`
		}
		if err := json.Unmarshal(data, &simple); err != nil {
			continue
		}

		if simple.URL == "" || simple.ResponseText == "" {
			continue
		}

		// Store in our cache format
		if err := c.Set(simple.URL, simple.ResponseText); err != nil {
			continue
		}

		loaded++
	}

	return loaded, nil
}
