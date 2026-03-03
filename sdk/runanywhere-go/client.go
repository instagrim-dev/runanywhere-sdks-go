package runanywhere

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errNilRequest is returned when a nil request is passed to Chat, ChatStream, or Speech.
var errNilRequest = errors.New("runanywhere: request must not be nil")

// Client is the HTTP client for the RunAnywhere OpenAI-compatible server.
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	timeout    time.Duration
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets the *http.Client used for requests. Nil is a no-op (keeps default client).
func WithHTTPClient(c *http.Client) ClientOption {
	return func(client *Client) {
		if c != nil {
			client.httpClient = c
		}
	}
}

// WithAPIKey sets an optional API key (Authorization: Bearer <key>).
func WithAPIKey(key string) ClientOption {
	return func(client *Client) {
		client.apiKey = key
	}
}

// WithTimeout sets the HTTP client timeout for requests (default: no timeout).
// Timeout is applied after all options, so order does not matter: use WithTimeout and WithHTTPClient in any order.
func WithTimeout(d time.Duration) ClientOption {
	return func(client *Client) {
		client.timeout = d
	}
}

// NewClient returns a client for the given base URL (e.g. "http://127.0.0.1:8080").
// Timeout is applied after all options so order of WithTimeout and WithHTTPClient does not matter.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	if c.timeout != 0 {
		c.httpClient.Timeout = c.timeout
	}
	return c
}

// ListModels returns the list of models from GET /v1/models.
func (c *Client) ListModels(ctx context.Context) (*ModelsResponse, error) {
	var out ModelsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/models", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Health returns the health status from GET /health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Chat sends a non-streaming chat completion request (stream is forced to false).
func (c *Client) Chat(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if req == nil {
		return nil, errNilRequest
	}
	reqCopy := *req
	reqCopy.Stream = false
	var out ChatCompletionResponse
	if err := c.do(ctx, http.MethodPost, "/v1/chat/completions", &reqCopy, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChatStream sends a streaming chat completion request and returns a reader for SSE chunks.
// The caller must call Close() on the returned reader when done.
func (c *Client) ChatStream(ctx context.Context, req *ChatCompletionRequest) (*ChatStreamReader, error) {
	if req == nil {
		return nil, errNilRequest
	}
	reqCopy := *req
	reqCopy.Stream = true
	body, err := c.doStream(ctx, http.MethodPost, "/v1/chat/completions", &reqCopy)
	if err != nil {
		return nil, err
	}
	return NewChatStreamReader(body), nil
}

// Transcribe sends audio to POST /v1/audio/transcriptions (multipart) and returns the transcript.
// filename is used as the form field filename (e.g. "audio.wav"). opts may be nil.
func (c *Client) Transcribe(ctx context.Context, audioReader io.Reader, filename string, opts *TranscribeOptions) (*TranscriptionResponse, error) {
	if audioReader == nil {
		return nil, errors.New("runanywhere: audio reader must not be nil")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, audioReader); err != nil {
		return nil, err
	}
	if opts != nil {
		if opts.Model != "" {
			_ = w.WriteField("model", opts.Model)
		}
		if opts.Language != "" {
			_ = w.WriteField("language", opts.Language)
		}
		if opts.ResponseFormat != "" {
			_ = w.WriteField("response_format", opts.ResponseFormat)
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	u, err := url.JoinPath(c.baseURL, "/v1/audio/transcriptions")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.decodeError(resp)
	}
	var out TranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Speech sends a text-to-speech request to POST /v1/audio/speech and returns the raw audio bytes.
func (c *Client) Speech(ctx context.Context, req *SpeechRequest) ([]byte, error) {
	if req == nil {
		return nil, errNilRequest
	}
	body, err := c.doBytesResponse(ctx, http.MethodPost, "/v1/audio/speech", req)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Embeddings sends a request to POST /v1/embeddings and returns the embeddings response.
func (c *Client) Embeddings(ctx context.Context, req *EmbeddingsRequest) (*EmbeddingsResponse, error) {
	if req == nil {
		return nil, errNilRequest
	}
	var out EmbeddingsResponse
	if err := c.do(ctx, http.MethodPost, "/v1/embeddings", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// doBytesResponse performs a request and returns the response body as bytes (for binary responses).
func (c *Client) doBytesResponse(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.decodeError(resp)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.decodeError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) doStream(ctx context.Context, method, path string, body interface{}) (io.ReadCloser, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := c.decodeError(resp)
		resp.Body.Close()
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, hasJSON bool) (*http.Request, error) {
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if hasJSON {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

func (c *Client) decodeError(resp *http.Response) error {
	var errBody ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	msg := errBody.Error.Message
	if msg == "" {
		msg = resp.Status
	}
	code := string(errBody.Error.Code)
	return &ServerError{StatusCode: resp.StatusCode, Message: msg, Type: errBody.Error.Type, Code: code}
}

// ServerError is returned when the server responds with a non-2xx status.
type ServerError struct {
	StatusCode int
	Message    string
	Type       string
	Code       string // optional; server may send as number or string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("server error %d: %s", e.StatusCode, e.Message)
}
