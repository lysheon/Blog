//go:build integration

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/migrations"
)

// The author workspace must return every own post — drafts and private posts
// included — while never leaking another author's writing, and the public
// list must stay unaffected by it.
func TestMyPostsWorkspace(t *testing.T) {
	container := newFlowContainer(t)

	anonymous := performJSON(t, container.Router(), http.MethodGet, "/api/v1/me/posts", nil, "", nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d body=%s", anonymous.Code, anonymous.Body.String())
	}

	author := registerFlowUser(t, container, "workspace-author")
	other := registerFlowUser(t, container, "workspace-other")

	// Creation order is oldest-first, so the expected workspace order is the
	// reverse: most recently written on top.
	mine := []struct {
		title, status, visibility string
	}{
		{"Workspace draft", "draft", "public"},
		{"Workspace private", "published", "private"},
		{"Workspace archived", "archived", "public"},
		{"Workspace published", "published", "public"},
	}
	for _, post := range mine {
		createFlowPost(t, container, author, post.title, post.status, post.visibility)
	}
	createFlowPost(t, container, other, "Someone else's story", "published", "public")

	all := performJSON(t, container.Router(), http.MethodGet, "/api/v1/me/posts", nil, author, nil)
	if all.Code != http.StatusOK {
		t.Fatalf("list mine status = %d body=%s", all.Code, all.Body.String())
	}
	titles := postTitles(t, all.Body.Bytes())
	if len(titles) != len(mine) {
		t.Fatalf("workspace returned %d posts %v, want the author's %d", len(titles), titles, len(mine))
	}
	for index, want := range []string{"Workspace published", "Workspace archived", "Workspace private", "Workspace draft"} {
		if titles[index] != want {
			t.Fatalf("workspace order = %v, want most recently edited first", titles)
		}
	}
	for _, title := range titles {
		if title == "Someone else's story" {
			t.Fatal("the workspace leaked another author's post")
		}
	}

	drafts := performJSON(t, container.Router(), http.MethodGet, "/api/v1/me/posts?status=draft", nil, author, nil)
	if drafts.Code != http.StatusOK {
		t.Fatalf("draft filter status = %d body=%s", drafts.Code, drafts.Body.String())
	}
	if titles := postTitles(t, drafts.Body.Bytes()); len(titles) != 1 || titles[0] != "Workspace draft" {
		t.Fatalf("draft filter = %v, want only the draft", titles)
	}

	invalid := performJSON(t, container.Router(), http.MethodGet, "/api/v1/me/posts?status=everything", nil, author, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter status = %d body=%s", invalid.Code, invalid.Body.String())
	}

	paged := performJSON(t, container.Router(), http.MethodGet, "/api/v1/me/posts?page=2&page_size=3", nil, author, nil)
	if paged.Code != http.StatusOK {
		t.Fatalf("paged status = %d body=%s", paged.Code, paged.Body.String())
	}
	if titles := postTitles(t, paged.Body.Bytes()); len(titles) != 1 {
		t.Fatalf("page 2 with size 3 = %v, want the single remaining post", titles)
	}

	// The workspace must not widen the public contract: readers still see only
	// published, public stories.
	public := performJSON(t, container.Router(), http.MethodGet, "/api/v1/posts?page=1&page_size=20", nil, "", nil)
	if public.Code != http.StatusOK {
		t.Fatalf("public list status = %d body=%s", public.Code, public.Body.String())
	}
	for _, title := range postTitles(t, public.Body.Bytes()) {
		if title == "Workspace draft" || title == "Workspace private" || title == "Workspace archived" {
			t.Fatalf("public list leaked %q", title)
		}
	}
}

func registerFlowUser(t *testing.T, container *Container, name string) string {
	t.Helper()
	response := performJSON(t, container.Router(), http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": name + "@example.com", "username": name, "password": "safe-password-123",
	}, "", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("register %s status = %d body=%s", name, response.Code, response.Body.String())
	}
	return nestedString(t, response.Body.Bytes(), "data", "access_token")
}

func createFlowPost(t *testing.T, container *Container, token, title, status, visibility string) {
	t.Helper()
	response := performJSON(t, container.Router(), http.MethodPost, "/api/v1/posts", map[string]any{
		"title": title, "content_markdown": "Body of " + title,
		"status": status, "visibility": visibility,
	}, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create %q status = %d body=%s", title, response.Code, response.Body.String())
	}
}

func postTitles(t *testing.T, payload []byte) []string {
	t.Helper()
	var body struct {
		Data struct {
			Posts []struct {
				Title string `json:"title"`
			} `json:"posts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode posts payload: %v", err)
	}
	titles := make([]string, len(body.Data.Posts))
	for index, post := range body.Data.Posts {
		titles[index] = post.Title
	}
	return titles
}

// newFlowContainer boots the full HTTP container against the integration
// MySQL/Redis and clears every table this package's flows write, so the tests
// stay repeatable against a persistent database.
func newFlowContainer(t *testing.T) *Container {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("TEST_MYSQL_DSN is required for integration tests")
	}
	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Fatal("TEST_REDIS_ADDR is required for integration tests")
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(): %v", err)
	}
	t.Setenv("APP_ENV", "dev")
	t.Setenv("APP_SERVICE_MODE", "api")
	t.Setenv("MYSQL_DSN", dsn)
	t.Setenv("JWT_SECRET", "integration-test-jwt-secret-at-least-32-bytes")
	t.Setenv("HTTP_TRUSTED_PROXIES", "127.0.0.0/8")
	t.Setenv("REDIS_ADDR", redisAddr)
	t.Setenv("REDIS_PASSWORD", os.Getenv("TEST_REDIS_PASSWORD"))
	t.Setenv("REDIS_KEY_PREFIX", "blog:integration:")
	t.Setenv("RATE_REGISTER_PER_MINUTE", "100")
	t.Setenv("RATE_LOGIN_PER_MINUTE", "100")
	t.Setenv("RATE_REFRESH_PER_MINUTE", "100")
	t.Setenv("RATE_COMMENT_PER_MINUTE", "100")
	t.Setenv("ARGON2_MEMORY_KIB", "8192")
	t.Setenv("ARGON2_ITERATIONS", "1")
	t.Setenv("ARGON2_PARALLELISM", "1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	container, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("bootstrap.New(): %v", err)
	}
	t.Cleanup(func() { container.Close(context.Background()) })
	if err := container.Redis.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush Redis: %v", err)
	}
	for _, statement := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
		"DELETE FROM background_jobs",
		"DELETE FROM comments",
		"DELETE FROM post_categories",
		"DELETE FROM post_tags",
		"DELETE FROM posts",
		"DELETE FROM categories",
		"DELETE FROM tags",
		"DELETE FROM refresh_tokens",
		"DELETE FROM user_profiles",
		"DELETE FROM users",
		"SET FOREIGN_KEY_CHECKS=1",
	} {
		if err := container.DB.Exec(statement).Error; err != nil {
			t.Fatalf("clean database with %q: %v", statement, err)
		}
	}
	return container
}
