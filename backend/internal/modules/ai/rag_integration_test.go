//go:build integration

package ai

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/database"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/platform/migrations"
)

// Stage 4 requires the retrieval pipeline to enforce the score threshold, the
// per-post and total chunk caps, current-version matching and public-only
// authorisation. Milvus only recalls candidates, so these tests drive
// selectCurrent with fabricated hits against real MySQL rows.

func TestSelectCurrentEnforcesRetrievalPolicy(t *testing.T) {
	db := newRAGTestDB(t)
	seed := seedRetrievalPosts(t, db)

	service := NewRAGService(db, nil, nil, nil, retrievalConfig())
	hits := []VectorHit{
		// Eligible current-version chunks for post A; the fourth must fall to the
		// per-post cap even though it clears the threshold.
		{PostID: seed.postA, ContentVersion: 2, Text: "A chunk 1", Score: 0.95},
		{PostID: seed.postA, ContentVersion: 2, Text: "A chunk 2", Score: 0.90},
		{PostID: seed.postA, ContentVersion: 2, Text: "A chunk 3", Score: 0.85},
		// A stale chunk of post A outranks B's best chunk but must be dropped.
		{PostID: seed.postA, ContentVersion: 1, Text: "A stale", Score: 0.93},
		// Ineligible posts with top scores must never become sources.
		{PostID: seed.privatePost, ContentVersion: 1, Text: "private", Score: 0.99},
		{PostID: seed.draftPost, ContentVersion: 1, Text: "draft", Score: 0.98},
		{PostID: seed.deletedPost, ContentVersion: 1, Text: "deleted", Score: 0.97},
		// Below the threshold.
		{PostID: seed.postA, ContentVersion: 2, Text: "A weak", Score: 0.10},
		// Post B fills the remaining total-cap slot.
		{PostID: seed.postB, ContentVersion: 1, Text: "B chunk 1", Score: 0.88},
		{PostID: seed.postB, ContentVersion: 1, Text: "B chunk 2", Score: 0.60},
	}

	selected, sources, sourceNumbers, err := service.selectCurrent(context.Background(), hits)
	if err != nil {
		t.Fatalf("selectCurrent(): %v", err)
	}

	gotTexts := make([]string, len(selected))
	for i, hit := range selected {
		gotTexts[i] = hit.Text
	}
	if want := []string{"A chunk 1", "A chunk 2", "B chunk 1"}; strings.Join(gotTexts, "|") != strings.Join(want, "|") {
		t.Fatalf("selected = %v, want %v (score order, per-post cap 2, total cap 3)", gotTexts, want)
	}

	if len(sources) != 2 || sources[0].PostID != seed.postA || sources[1].PostID != seed.postB {
		t.Fatalf("sources = %+v, want deduplicated [A, B]", sources)
	}
	if sources[0].Title != "Post A" || sources[0].Slug == "" {
		t.Fatalf("source A = %+v, want title and slug from MySQL", sources[0])
	}
	if sourceNumbers[seed.postA] != 0 || sourceNumbers[seed.postB] != 1 {
		t.Fatalf("sourceNumbers = %v, want A→0 B→1", sourceNumbers)
	}
}

func TestAskGroundsChatContextInDeduplicatedSources(t *testing.T) {
	db := newRAGTestDB(t)
	seed := seedRetrievalPosts(t, db)

	chat := &spyChat{}
	vectors := &stubVectorStore{hits: []VectorHit{
		{PostID: seed.postA, ContentVersion: 2, Text: "A chunk 1", Score: 0.95},
		{PostID: seed.postA, ContentVersion: 2, Text: "A chunk 2", Score: 0.90},
		{PostID: seed.postB, ContentVersion: 1, Text: "B chunk 1", Score: 0.88},
	}}
	service := NewRAGService(db, &stubEmbedder{vector: unitVector(4)}, chat, vectors, retrievalConfig())

	response, err := service.Ask(context.Background(), "两篇文章都讲了什么？")
	if err != nil {
		t.Fatalf("Ask(): %v", err)
	}
	if chat.calls != 1 || len(response.Sources) != 2 {
		t.Fatalf("chat calls = %d sources = %d, want one grounded call with two sources", chat.calls, len(response.Sources))
	}

	userMessage := chat.messages[len(chat.messages)-1].Content
	if !strings.Contains(userMessage, "两篇文章都讲了什么？") {
		t.Fatal("chat context lost the user question")
	}
	for _, match := range regexp.MustCompile(`\[SOURCE (\d+)]`).FindAllStringSubmatch(userMessage, -1) {
		number, _ := strconv.Atoi(match[1])
		if number < 1 || number > len(response.Sources) {
			t.Fatalf("chat context cites [SOURCE %d] but only %d sources are returned:\n%s", number, len(response.Sources), userMessage)
		}
	}
	if !strings.Contains(userMessage, "[SOURCE 2]\nB chunk 1") {
		t.Fatalf("post B's excerpt must carry source number 2:\n%s", userMessage)
	}
}

type retrievalSeed struct {
	postA, postB, privatePost, draftPost, deletedPost string
}

func retrievalConfig() config.AIConfig {
	return config.AIConfig{
		RAGEnabled: true,
		Embedding:  config.EmbeddingConfig{Model: "embed", Dimensions: 4},
		Chat:       config.ChatModelConfig{Model: "chat", MaxTokens: 100},
		RAG: config.RAGConfig{
			TopK: 20, FinalChunks: 3, MaxChunksPerPost: 2,
			ScoreThreshold: 0.5, MaxQuestionChars: 2000,
		},
	}
}

func newRAGTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := getenvRequired(t, "TEST_MYSQL_DSN")
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 5,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute,
	}, "dev")
	if err != nil {
		t.Fatalf("database.New(): %v", err)
	}
	t.Cleanup(func() { database.Close(db) })
	return db
}

func seedRetrievalPosts(t *testing.T, db *gorm.DB) retrievalSeed {
	t.Helper()
	suffix := strings.ToLower(ids.MustNewULID())
	user := &domain.User{
		PublicID: ids.MustNewULID(), Email: "rag-" + suffix + "@example.test",
		EmailNormalized: "rag-" + suffix + "@example.test",
		Username:        "rag_" + suffix[:12], PasswordHash: "integration-only",
		Role: "user", Status: "active", TokenVersion: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("author_id = ?", user.ID).Delete(&domain.Post{})
		db.Where("id = ?", user.ID).Delete(&domain.User{})
	})

	postA := newIntegrationPost(user.ID, ids.MustNewULID(), "Post A", "current content of A")
	postA.ContentVersion = 2
	postB := newIntegrationPost(user.ID, ids.MustNewULID(), "Post B", "content of B")
	private := newIntegrationPost(user.ID, ids.MustNewULID(), "Private", "private content")
	private.Visibility = "private"
	draft := newIntegrationPost(user.ID, ids.MustNewULID(), "Draft", "draft content")
	draft.Status = "draft"
	deleted := newIntegrationPost(user.ID, ids.MustNewULID(), "Deleted", "deleted content")
	now := time.Now().UTC()
	deleted.DeletedAt = &now

	for _, post := range []*domain.Post{postA, postB, private, draft, deleted} {
		if err := db.Create(post).Error; err != nil {
			t.Fatalf("create post %s: %v", post.Title, err)
		}
	}
	return retrievalSeed{
		postA: postA.PublicID, postB: postB.PublicID,
		privatePost: private.PublicID, draftPost: draft.PublicID, deletedPost: deleted.PublicID,
	}
}
