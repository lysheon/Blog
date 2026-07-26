package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/platform/observability"
	"github.com/lsy/blog/internal/platform/openaicompat"
	"github.com/lsy/blog/internal/shared/apperr"
)

var sourceMarker = regexp.MustCompile(`\[SOURCE (\d+)]`)

func TestBuildSourceContextNumbersExcerptsBySource(t *testing.T) {
	t.Parallel()

	// Two chunks of the same post collapse into a single returned source, so the
	// second chunk must not advance the citation number.
	selected := []VectorHit{
		{PostID: "post-a", Text: "first excerpt from A"},
		{PostID: "post-b", Text: "only excerpt from B"},
		{PostID: "post-a", Text: "second excerpt from A"},
	}
	sourceNumbers := map[string]int{"post-a": 0, "post-b": 1}
	sourceCount := len(sourceNumbers)

	context := buildSourceContext(selected, sourceNumbers)

	for _, match := range sourceMarker.FindAllStringSubmatch(context, -1) {
		number, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("unparsable source marker %q", match[0])
		}
		if number < 1 || number > sourceCount {
			t.Fatalf("citation [%d] has no matching source among %d sources:\n%s", number, sourceCount, context)
		}
	}
	if strings.Count(context, "[SOURCE 1]") != 2 {
		t.Fatalf("both excerpts of post-a should share citation [1]:\n%s", context)
	}
	if strings.Count(context, "[SOURCE 2]") != 1 {
		t.Fatalf("post-b should be cited exactly once as [2]:\n%s", context)
	}
	for _, hit := range selected {
		if !strings.Contains(context, hit.Text) {
			t.Fatalf("context dropped excerpt %q:\n%s", hit.Text, context)
		}
	}
}

func TestDisabledAIReportsStableContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		module *Module
	}{
		{name: "ask without RAG", path: "/api/v1/ai/ask", module: NewModule(nil, nil, true)},
		{name: "reindex without indexing", path: "/api/v1/ai/reindex", module: NewModule(nil, nil, false)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := testAIRouter(t, test.module)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"question":"anything"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)

			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), http.StatusServiceUnavailable)
			}
			if !strings.Contains(response.Body.String(), string(apperr.CodeAINotEnabled)) {
				t.Fatalf("body = %s, want %s", response.Body.String(), apperr.CodeAINotEnabled)
			}
		})
	}
}

// testAIRouter wires the module behind an always-failing AI limiter. A disabled
// deployment must answer ai_not_enabled without depending on Redis.
func testAIRouter(t *testing.T, module *Module) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := observability.New(config.ObservabilityConfig{LogLevel: "error", LogFormat: "json", RequestIDHeader: "X-Request-ID"})
	router := gin.New()
	router.Use(httpserver.RequestID("X-Request-ID"), httpserver.ErrorHandler(logger))
	failClosedLimiter := func(c *gin.Context) {
		c.Error(apperr.AIUnavailable(errors.New("rate limiter backend is unavailable")))
		c.Abort()
	}
	allowAdmin := func(c *gin.Context) { c.Next() }
	module.Register(router, allowAdmin, failClosedLimiter)
	return router
}

// --- Ask retrieval-policy units that need no database ---

type stubEmbedder struct {
	vector []float32
	err    error
	calls  int
}

func (s *stubEmbedder) Embed(_ context.Context, _ string, _ int, inputs []string) ([][]float32, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = s.vector
	}
	return out, nil
}

type stubVectorStore struct {
	hits      []VectorHit
	err       error
	gotVector []float32
	gotTopK   int
}

func (s *stubVectorStore) Ensure(context.Context) error                             { return nil }
func (s *stubVectorStore) ReplacePost(context.Context, string, []VectorChunk) error { return nil }
func (s *stubVectorStore) DeletePost(context.Context, string) error                 { return nil }
func (s *stubVectorStore) Close(context.Context) error                              { return nil }
func (s *stubVectorStore) Search(_ context.Context, vector []float32, topK int) ([]VectorHit, error) {
	s.gotVector, s.gotTopK = vector, topK
	if s.err != nil {
		return nil, s.err
	}
	return s.hits, nil
}

type spyChat struct {
	calls    int
	messages []openaicompat.Message
}

func (s *spyChat) Chat(_ context.Context, _ string, _ int, messages []openaicompat.Message) (string, error) {
	s.calls++
	s.messages = messages
	return "stub answer", nil
}

func ragTestConfig() config.AIConfig {
	return config.AIConfig{
		RAGEnabled: true,
		Embedding:  config.EmbeddingConfig{Model: "embed", Dimensions: 4},
		Chat:       config.ChatModelConfig{Model: "chat", MaxTokens: 100},
		RAG: config.RAGConfig{
			TopK: 10, FinalChunks: 3, MaxChunksPerPost: 2,
			ScoreThreshold: 0.5, MaxQuestionChars: 50,
		},
	}
}

func appErrCode(t *testing.T, err error) apperr.Code {
	t.Helper()
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("err = %v, want *apperr.AppError", err)
	}
	return appErr.Code
}

func TestAskRejectsInvalidQuestionsBeforeAnyUpstreamCall(t *testing.T) {
	t.Parallel()

	embedder := &stubEmbedder{vector: []float32{1, 0, 0, 0}}
	chat := &spyChat{}
	service := NewRAGService(nil, embedder, chat, &stubVectorStore{}, ragTestConfig())

	for _, question := range []string{"", "   ", strings.Repeat("长", 51)} {
		if _, err := service.Ask(context.Background(), question); appErrCode(t, err) != apperr.CodeValidation {
			t.Fatalf("question %q: code = %v, want validation", question, appErrCode(t, err))
		}
	}
	if embedder.calls != 0 || chat.calls != 0 {
		t.Fatalf("embedder calls = %d chat calls = %d, invalid questions must not reach upstreams", embedder.calls, chat.calls)
	}
}

// A nil *gorm.DB doubles as the proof that a below-threshold recall answers
// without ever building a database query.
func TestAskBelowThresholdAnswersWithoutChatOrDatabase(t *testing.T) {
	t.Parallel()

	chat := &spyChat{}
	vectors := &stubVectorStore{hits: []VectorHit{
		{PostID: "post-a", ContentVersion: 1, Text: "irrelevant", Score: 0.49},
		{PostID: "post-b", ContentVersion: 1, Text: "also irrelevant", Score: 0.2},
	}}
	service := NewRAGService(nil, &stubEmbedder{vector: []float32{1, 0, 0, 0}}, chat, vectors, ragTestConfig())

	response, err := service.Ask(context.Background(), "有什么相关内容？")
	if err != nil {
		t.Fatalf("Ask(): %v", err)
	}
	if chat.calls != 0 {
		t.Fatalf("chat calls = %d, empty recall must not spend Chat tokens", chat.calls)
	}
	if !strings.Contains(response.Answer, "couldn't find") {
		t.Fatalf("answer = %q, want the explicit insufficient-information reply", response.Answer)
	}
	if response.Sources == nil || len(response.Sources) != 0 {
		t.Fatalf("sources = %#v, want a present-but-empty list", response.Sources)
	}
	if vectors.gotTopK != 10 {
		t.Fatalf("search topK = %d, want the configured 10", vectors.gotTopK)
	}
}

func TestAskWrapsUpstreamFailuresAsAIUnavailable(t *testing.T) {
	t.Parallel()

	embedFailure := NewRAGService(nil, &stubEmbedder{err: errors.New("embed down")}, &spyChat{}, &stubVectorStore{}, ragTestConfig())
	if _, err := embedFailure.Ask(context.Background(), "问题"); appErrCode(t, err) != apperr.CodeAIUnavailable {
		t.Fatalf("embed failure code = %v, want ai_unavailable", appErrCode(t, err))
	}

	searchFailure := NewRAGService(nil, &stubEmbedder{vector: []float32{1, 0, 0, 0}}, &spyChat{}, &stubVectorStore{err: errors.New("milvus down")}, ragTestConfig())
	if _, err := searchFailure.Ask(context.Background(), "问题"); appErrCode(t, err) != apperr.CodeAIUnavailable {
		t.Fatalf("search failure code = %v, want ai_unavailable", appErrCode(t, err))
	}
}
