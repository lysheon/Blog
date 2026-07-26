package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbedOrdersByIndexAndValidatesDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "embed" || body.Dimensions != 2 || len(body.Input) != 2 {
			t.Fatalf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	client, err := New(server.URL+"/v1", "secret", time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Embed(context.Background(), "embed", 2, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Fatalf("vectors not ordered by index: %#v", vectors)
	}
}

func TestClientRetriesRetryableStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answer"}}]}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", 2*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "answer" || calls.Load() != 2 {
		t.Fatalf("answer=%q calls=%d", answer, calls.Load())
	}
}

func TestClientDoesNotRetryOrdinaryBadRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "invalid", http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", time.Second, 3)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hello"}})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

// Stage 4 requires the provider contract to cover empty responses, 4xx, 429,
// 5xx, timeout and cancellation for both Chat and Embedding.

func TestChatRejectsEmptyResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "no choices", body: `{"choices":[]}`},
		{name: "blank content", body: `{"choices":[{"message":{"role":"assistant","content":"   "}}]}`},
		{name: "missing message", body: `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, respondWith(http.StatusOK, test.body), time.Second, 0)
			if _, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hi"}}); err == nil {
				t.Fatal("an empty completion must be reported as an upstream failure")
			}
		})
	}
}

func TestChatRetriesRateLimitAndReportsStatus(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answer"}}]}`))
	}, 5*time.Second, 1)

	answer, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	if answer != "answer" || calls.Load() != 2 {
		t.Fatalf("answer=%q calls=%d, want one retry after 429", answer, calls.Load())
	}
}

func TestClientSurfacesStatusErrorWithoutLeakingWholeBody(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, respondWith(http.StatusForbidden, `{"error":{"message":"key disabled"}}`), time.Second, 0)
	_, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hi"}})

	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if status.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d, want %d", status.StatusCode, http.StatusForbidden)
	}
	if len(status.Body) > 1000 {
		t.Fatalf("provider body must stay bounded, got %d bytes", len(status.Body))
	}
}

func TestClientGivesUpAfterExhaustingRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "still busy", http.StatusBadGateway)
	}, 5*time.Second, 2)

	if _, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("exhausted retries must return the last error")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want the initial attempt plus 2 retries", calls.Load())
	}
}

func TestClientTimesOutSlowProvider(t *testing.T) {
	t.Parallel()

	// The delay only needs to outlast the client timeout; it stays bounded so the
	// server can always finish its handler and shut down cleanly.
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"too late"}}]}`))
	}, 50*time.Millisecond, 0)

	if _, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("a provider slower than the timeout must fail the call")
	}
}

func TestClientStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}, time.Second, 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Chat(ctx, "chat", 100, []Message{{Role: "user", Content: "hi"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("calls = %d, a cancelled context must not reach the provider", calls.Load())
	}
}

func TestEmbedRejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "wrong dimension", body: `{"data":[{"index":0,"embedding":[1,2,3]},{"index":1,"embedding":[1,2,3]}]}`},
		{name: "fewer vectors than inputs", body: `{"data":[{"index":0,"embedding":[1,2]}]}`},
		{name: "duplicate index", body: `{"data":[{"index":0,"embedding":[1,2]},{"index":0,"embedding":[3,4]}]}`},
		{name: "index out of range", body: `{"data":[{"index":0,"embedding":[1,2]},{"index":7,"embedding":[3,4]}]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, respondWith(http.StatusOK, test.body), time.Second, 0)
			if _, err := client.Embed(context.Background(), "embed", 2, []string{"a", "b"}); err == nil {
				t.Fatalf("malformed embedding response must fail: %s", test.body)
			}
		})
	}
}

func TestEmbedValidatesArgumentsBeforeCallingProvider(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}, time.Second, 0)

	tests := []struct {
		name       string
		model      string
		dimensions int
		inputs     []string
	}{
		{name: "empty model", model: " ", dimensions: 2, inputs: []string{"a"}},
		{name: "non-positive dimensions", model: "embed", dimensions: 0, inputs: []string{"a"}},
		{name: "no inputs", model: "embed", dimensions: 2, inputs: nil},
	}

	for _, test := range tests {
		if _, err := client.Embed(context.Background(), test.model, test.dimensions, test.inputs); err == nil {
			t.Fatalf("%s must be rejected locally", test.name)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("calls = %d, invalid arguments must not reach the provider", calls.Load())
	}
}

func TestNewRejectsUnusableEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		apiKey  string
	}{
		{name: "missing base URL", baseURL: "", apiKey: "secret"},
		{name: "missing API key", baseURL: "https://provider.example/v1", apiKey: ""},
		{name: "non-HTTP scheme", baseURL: "ftp://provider.example/v1", apiKey: "secret"},
		{name: "no host", baseURL: "https:///v1", apiKey: "secret"},
	}

	for _, test := range tests {
		if _, err := New(test.baseURL, test.apiKey, time.Second, 0); err == nil {
			t.Fatalf("%s must be rejected at construction", test.name)
		}
	}
}

func TestChatSendsConfiguredNonStreamingRequest(t *testing.T) {
	t.Parallel()

	var received chatRequest
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %q, want the chat completions endpoint", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":" answer "}}]}`))
	}, time.Second, 0)

	answer, err := client.Chat(context.Background(), "chat", 42, []Message{{Role: "system", Content: "rules"}, {Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	if answer != "answer" {
		t.Fatalf("answer = %q, want the trimmed content", answer)
	}
	if received.Model != "chat" || received.MaxTokens != 42 || received.Stream || len(received.Messages) != 2 {
		t.Fatalf("request = %+v, want the configured non-streaming request", received)
	}
}

func respondWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc, timeout time.Duration, maxRetries int) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(server.URL, "secret", timeout, maxRetries)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return client
}
