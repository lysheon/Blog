package ai

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/shared/apperr"
)

type Module struct {
	repo            *Repository
	rag             *RAGService
	indexingEnabled bool
}

func NewModule(repo *Repository, rag *RAGService, indexingEnabled bool) *Module {
	return &Module{repo: repo, rag: rag, indexingEnabled: indexingEnabled}
}

func (m *Module) Register(r *gin.Engine, adminMW gin.HandlerFunc, aiLimit gin.HandlerFunc) {
	v1 := r.Group("/api/v1/ai")
	v1.POST("/reindex", adminMW, m.reindex)
	// The strict AI limiter exists to bound paid upstream calls. A deployment with
	// RAG disabled answers immediately without any upstream work, so it is mounted
	// without the limiter; otherwise a Redis outage would mask the stable
	// ai_not_enabled contract behind a fail-closed ai_unavailable response.
	if m.rag != nil {
		v1.POST("/ask", aiLimit, m.ask)
		return
	}
	v1.POST("/ask", m.ask)
}

func (m *Module) reindex(c *gin.Context) {
	if !m.indexingEnabled {
		c.Error(apperr.AINotEnabled())
		return
	}
	count, err := m.repo.Backfill(c.Request.Context())
	if err != nil {
		c.Error(apperr.Internal(err, ""))
		return
	}
	httpserver.OK(c, gin.H{"enqueued": count})
}

func (m *Module) ask(c *gin.Context) {
	if m.rag == nil {
		c.Error(apperr.AINotEnabled())
		return
	}
	var input AskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	response, err := m.rag.Ask(c.Request.Context(), input.Question)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, response)
}

var _ = http.StatusOK
