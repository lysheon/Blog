package ai

import (
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
