package recommender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func ValidateServerURL(rawURL string) (normalizedURL string, err error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("server URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid URL scheme %q: must start with http:// or https://", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("URL must include a host")
	}
	return strings.TrimRight(trimmed, "/"), nil
}

func authHeader(apiKey string) string {
	return fmt.Sprintf(`MediaBrowser Token="%s"`, apiKey)
}

func (s *StateManager) newHTTPClient() *http.Client {
	return &http.Client{Timeout: s.Config.HTTPRequestTimeout}
}

func doWithRetry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if attempt < maxAttempts-1 {
			delay := baseDelay * time.Duration(1<<uint(attempt))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}

func getJellyfin[T any](s *StateManager, endpoint string) (T, error) {
	var result T
	reqURL := fmt.Sprintf("%s%s", s.Config.ServerURL, endpoint)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return result, fmt.Errorf("create GET %s: %w", endpoint, err)
	}
	req.Header.Set("Authorization", authHeader(s.Config.APIKey))
	resp, err := s.newHTTPClient().Do(req)
	if err != nil {
		return result, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return result, fmt.Errorf("GET %s: HTTP %d %s (body: %.200s)", endpoint, resp.StatusCode, resp.Status, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("decode GET %s: %w", endpoint, err)
	}
	return result, nil
}

func postJellyfin[T any](s *StateManager, endpoint string, payload any) (T, error) {
	var result T
	reqURL := fmt.Sprintf("%s%s", s.Config.ServerURL, endpoint)
	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return result, fmt.Errorf("marshal POST %s: %w", endpoint, err)
		}
		bodyReader = bytes.NewBuffer(b)
	}
	req, err := http.NewRequest(http.MethodPost, reqURL, bodyReader)
	if err != nil {
		return result, fmt.Errorf("create POST %s: %w", endpoint, err)
	}
	req.Header.Set("Authorization", authHeader(s.Config.APIKey))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.newHTTPClient().Do(req)
	if err != nil {
		return result, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return result, fmt.Errorf("POST %s: HTTP %d %s (body: %.200s)", endpoint, resp.StatusCode, resp.Status, string(body))
	}
	if resp.ContentLength == 0 || resp.StatusCode == http.StatusNoContent {
		return result, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if err == io.EOF {
			return result, nil
		}
		return result, fmt.Errorf("decode POST %s: %w", endpoint, err)
	}
	return result, nil
}

func (s *StateManager) deleteJellyfin(endpoint string) error {
	reqURL := fmt.Sprintf("%s%s", s.Config.ServerURL, endpoint)
	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("create DELETE %s: %w", endpoint, err)
	}
	req.Header.Set("Authorization", authHeader(s.Config.APIKey))
	resp, err := s.newHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: HTTP %d %s (body: %.200s)", endpoint, resp.StatusCode, resp.Status, string(body))
	}
	return nil
}
