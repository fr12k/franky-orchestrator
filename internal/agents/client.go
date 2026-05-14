package agents

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a read-only HTTP client for agent APIs.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new Client with a 10-second timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetTranscript fetches the transcript from an agent at apiURL.
func (c *Client) GetTranscript(apiURL string) ([]byte, error) {
	url := apiURL + "/transcript"
	return c.doGet(url)
}

// GetRole fetches the role info from an agent at apiURL.
func (c *Client) GetRole(apiURL string) ([]byte, error) {
	url := apiURL + "/role"
	return c.doGet(url)
}

// GetUsage fetches usage counters from an agent at apiURL.
func (c *Client) GetUsage(apiURL string) ([]byte, error) {
	url := apiURL + "/usage"
	return c.doGet(url)
}

// GetSession fetches session info from an agent at apiURL.
func (c *Client) GetSession(apiURL string) ([]byte, error) {
	url := apiURL + "/session"
	return c.doGet(url)
}

// GetSessions fetches session list from an agent at apiURL.
func (c *Client) GetSessions(apiURL string) ([]byte, error) {
	url := apiURL + "/sessions"
	return c.doGet(url)
}

// GetDesignDocs fetches design docs from an agent at apiURL.
func (c *Client) GetDesignDocs(apiURL string) ([]byte, error) {
	url := apiURL + "/design-docs"
	return c.doGet(url)
}

// doGet performs a GET request and returns the raw response body.
func (c *Client) doGet(url string) ([]byte, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("agent unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // limit to 10MB
}
