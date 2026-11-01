package ark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"clovergo/internal/domain"
)

// Embedder calls Ark's multimodal embedding endpoint with text input. Each
// call embeds one text: the endpoint fuses multiple inputs into a single
// vector, so batching texts in one request is not possible.
type Embedder struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	httpClient *http.Client
}

func NewEmbedder(baseURL, apiKey, model string, dimensions int, timeout time.Duration) *Embedder {
	return &Embedder{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), apiKey: strings.TrimSpace(apiKey),
		model: strings.TrimSpace(model), dimensions: dimensions,
		httpClient: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (e *Embedder) Model() string   { return e.model }
func (e *Embedder) Dimensions() int { return e.dimensions }

// Configured reports whether semantic search is enabled. An empty model turns
// it off deliberately; the health endpoint reports which mode is in use.
func (e *Embedder) Configured() bool {
	return e.model != "" && (&Client{baseURL: e.baseURL, apiKey: e.apiKey, model: e.model}).ConfigurationError() == nil
}

// Embed returns the L2-normalized embedding of text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, domain.TokenUsage, error) {
	if !e.Configured() {
		return nil, domain.TokenUsage{}, errors.New("ARK_EMBEDDING_MODEL, ARK_API_KEY, and an official ARK_BASE_URL must be configured")
	}
	if strings.TrimSpace(text) == "" {
		return nil, domain.TokenUsage{}, fmt.Errorf("%w: embedding text is empty", ErrInvalidResponse)
	}
	payload, err := json.Marshal(map[string]any{
		"model":      e.model,
		"dimensions": e.dimensions,
		"input":      []map[string]string{{"type": "text", "text": text}},
	})
	if err != nil {
		return nil, domain.TokenUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings/multimodal", bytes.NewReader(payload))
	if err != nil {
		return nil, domain.TokenUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.httpClient.Do(req)
	if err != nil {
		var netErr net.Error
		timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
		return nil, domain.TokenUsage{}, &UpstreamError{Timeout: timeout, Cause: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, domain.TokenUsage{}, &UpstreamError{Cause: fmt.Errorf("read Ark embedding response: %w", err)}
	}
	requestID := firstHeader(resp.Header, "X-Request-Id", "X-Tt-Logid")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, domain.TokenUsage{}, &UpstreamError{StatusCode: resp.StatusCode, RequestID: requestID}
	}
	var response struct {
		Data struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens *int `json:"prompt_tokens"`
			TotalTokens  *int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, domain.TokenUsage{}, fmt.Errorf("%w: embedding response is not JSON: %v", ErrInvalidResponse, err)
	}
	usage := domain.TokenUsage{InputTokens: response.Usage.PromptTokens, TotalTokens: response.Usage.TotalTokens}
	if len(response.Data.Embedding) != e.dimensions {
		return nil, usage, fmt.Errorf("%w: embedding has %d dimensions, expected %d", ErrInvalidResponse, len(response.Data.Embedding), e.dimensions)
	}
	var norm float64
	for _, value := range response.Data.Embedding {
		norm += value * value
	}
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, usage, fmt.Errorf("%w: embedding has no usable magnitude", ErrInvalidResponse)
	}
	norm = math.Sqrt(norm)
	vector := make([]float32, len(response.Data.Embedding))
	for i, value := range response.Data.Embedding {
		vector[i] = float32(value / norm)
	}
	return vector, usage, nil
}
