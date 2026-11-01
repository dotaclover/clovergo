package ark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"clovergo/internal/domain"
)

var ErrInvalidResponse = errors.New("Ark returned an invalid structured response")

// UpstreamError describes a failed Ark HTTP exchange precisely enough for the
// service to tell an invalid key from rate limiting or a timeout.
type UpstreamError struct {
	StatusCode int
	RequestID  string
	Timeout    bool
	Cause      error
}

func (e *UpstreamError) Error() string {
	message := "Ark request failed"
	switch {
	case e.Timeout:
		message = "Ark request timed out"
	case e.StatusCode != 0:
		message = fmt.Sprintf("Ark upstream returned HTTP %d", e.StatusCode)
	}
	if e.RequestID != "" {
		message += " (request_id=" + e.RequestID + ")"
	}
	if e.Cause != nil && e.StatusCode == 0 && !e.Timeout {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *UpstreamError) Unwrap() error { return e.Cause }

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) Configured() bool { return c.ConfigurationError() == nil }
func (c *Client) Model() string    { return c.model }

func (c *Client) ConfigurationError() error {
	missing := make([]string, 0, 3)
	if c.baseURL == "" {
		missing = append(missing, "ARK_BASE_URL")
	}
	if c.apiKey == "" {
		missing = append(missing, "ARK_API_KEY")
	}
	if c.model == "" {
		missing = append(missing, "ARK_MODEL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s must be configured", strings.Join(missing, ", "))
	}
	parsed, err := url.Parse(c.baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("ARK_BASE_URL must be a valid HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "volces.com" && !strings.HasSuffix(host, ".volces.com") {
		return errors.New("ARK_BASE_URL must use an official volces.com host")
	}
	return nil
}

type toolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
	// DisableThinking turns off the model's reasoning pass for this call. It is
	// off by default, so callers that do not ask keep the server's default
	// behaviour. Extraction sets it: reading a source and filling in a schema is
	// not a reasoning task, and the reasoning tokens are billed against the same
	// completion budget that has to carry the candidates.
	DisableThinking bool
	// MaxTokens caps one response. Zero leaves the server-side default (128k for
	// the configured model), which is far above what a single call should ever
	// need; setting it makes a runaway response fail as a truncation instead of
	// as a timeout.
	MaxTokens int
}

func (c *Client) callTool(
	ctx context.Context,
	system, user string,
	tool toolDefinition,
) ([]byte, domain.TokenUsage, error) {
	if err := c.ConfigurationError(); err != nil {
		return nil, domain.TokenUsage{}, err
	}
	requestPayload := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0.1,
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters,
			},
		}},
		"tool_choice": map[string]any{
			"type": "function", "function": map[string]string{"name": tool.Name},
		},
	}
	if tool.DisableThinking {
		requestPayload["thinking"] = map[string]any{"type": "disabled"}
	}
	if tool.MaxTokens > 0 {
		requestPayload["max_tokens"] = tool.MaxTokens
	}
	message, usage, err := c.send(ctx, requestPayload)
	if err != nil {
		return nil, usage, err
	}
	for _, call := range message.ToolCalls {
		if call.Type == "function" && call.Function.Name == tool.Name && strings.TrimSpace(call.Function.Arguments) != "" {
			return []byte(call.Function.Arguments), usage, nil
		}
	}
	return nil, usage, fmt.Errorf("%w: required tool call %s is missing", ErrInvalidResponse, tool.Name)
}

// send posts one chat completion request and returns the first choice's
// message. Every model call goes through here, so status, size, truncation
// and usage are handled in one place.
func (c *Client) send(ctx context.Context, requestPayload map[string]any) (domain.ChatMessage, domain.TokenUsage, error) {
	payload, err := json.Marshal(requestPayload)
	if err != nil {
		return domain.ChatMessage{}, domain.TokenUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return domain.ChatMessage{}, domain.TokenUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		var netErr net.Error
		timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
		return domain.ChatMessage{}, domain.TokenUsage{}, &UpstreamError{Timeout: timeout, Cause: err}
	}
	defer resp.Body.Close()
	const maxResponseBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		var netErr net.Error
		timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
		return domain.ChatMessage{}, domain.TokenUsage{}, &UpstreamError{Timeout: timeout, Cause: fmt.Errorf("read Ark response: %w", err)}
	}
	if len(body) > maxResponseBytes {
		return domain.ChatMessage{}, domain.TokenUsage{}, fmt.Errorf("%w: response exceeds %d bytes", ErrInvalidResponse, maxResponseBytes)
	}
	requestID := firstHeader(resp.Header, "X-Request-Id", "X-Tt-Logid")
	var response struct {
		Usage struct {
			PromptTokens     *int `json:"prompt_tokens"`
			CompletionTokens *int `json:"completion_tokens"`
			TotalTokens      *int `json:"total_tokens"`
		} `json:"usage"`
		Choices []struct {
			FinishReason string             `json:"finish_reason"`
			Message      domain.ChatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return domain.ChatMessage{}, domain.TokenUsage{}, &UpstreamError{StatusCode: resp.StatusCode, RequestID: requestID}
		}
		return domain.ChatMessage{}, domain.TokenUsage{}, fmt.Errorf("%w: response is not JSON", ErrInvalidResponse)
	}
	usage := domain.TokenUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.ChatMessage{}, usage, &UpstreamError{StatusCode: resp.StatusCode, RequestID: requestID}
	}
	if len(response.Choices) == 0 {
		return domain.ChatMessage{}, usage, fmt.Errorf("%w: choices are empty", ErrInvalidResponse)
	}
	if response.Choices[0].FinishReason == "length" {
		return domain.ChatMessage{}, usage, fmt.Errorf("%w: output was truncated at the model token limit (finish_reason=length)", ErrInvalidResponse)
	}
	return response.Choices[0].Message, usage, nil
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}
