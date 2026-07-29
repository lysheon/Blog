import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { api, ApiError } from '../api'
import { EmptyState, ErrorState } from '../components'
import type { PagedPosts, Post } from '../types'

const filters = [
  { value: '', label: 'All' },
  { value: 'draft', label: 'Drafts' },
  { value: 'published', label: 'Published' },
  { value: 'archived', label: 'Archived' },
] as const

function publicPath(post: Post) {
  return post.status === 'published' && post.visibility === 'public' ? `/posts/${post.slug}` : null
}

export function MyPostsPage() {
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [result, setResult] = useState<PagedPosts | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    api.listMyPosts(page, status)
      .then((data) => { if (!cancelled) setResult(data) })
      .catch((caught) => {
        if (!cancelled) setError(caught instanceof ApiError ? caught.message : 'Could not load your stories.')
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [page, status])

  function applyFilter(value: string) {
    setStatus(value)
    setPage(1)
  }

  return <main className="page-shell">
    <section className="section-block">
      <div className="section-heading">
        <div><span className="eyebrow">Your studio</span><h2>My stories</h2></div>
        {result && <span className="muted">{result.pagination.total} total</span>}
      </div>

      <div className="ask-actions" role="group" aria-label="Filter by status">
        {filters.map((filter) => (
          <button key={filter.value} type="button"
            className={status === filter.value ? 'button compact' : 'button ghost compact'}
            aria-pressed={status === filter.value}
            onClick={() => applyFilter(filter.value)}>{filter.label}</button>
        ))}
      </div>

      {error && <ErrorState message={error} retry={() => applyFilter(status)} />}
      {!error && loading && !result && <p className="muted">Loading your stories…</p>}
      {!error && result && result.posts.length === 0 && (
        <EmptyState title="Nothing here yet">
          Stories you write appear here across every status. <Link to="/write">Start one now.</Link>
        </EmptyState>
      )}
      {!error && result && result.posts.length > 0 && (
        <div className="taxonomy-list">
          {result.posts.map((post) => {
            const readable = publicPath(post)
            return <div className="taxonomy-row" key={post.public_id}>
              <div>
                <strong>{post.title}</strong>
                <span>{post.status} · {post.visibility} · edited {new Date(post.updated_at).toLocaleDateString()}</span>
                {post.summary && <p>{post.summary}</p>}
              </div>
              <div>
                <Link className="text-button" to={`/write?edit=${encodeURIComponent(post.slug)}`}>Edit</Link>
                {readable && <Link className="text-button" to={readable}>View</Link>}
              </div>
            </div>
          })}
        </div>
      )}

      {result && result.pagination.total_pages > 1 && (
        <nav className="pagination" aria-label="Workspace pages">
          <button disabled={page <= 1} onClick={() => setPage(page - 1)}>← Newer</button>
          <span>{page} / {result.pagination.total_pages}</span>
          <button disabled={page >= result.pagination.total_pages} onClick={() => setPage(page + 1)}>Older →</button>
        </nav>
      )}
    </section>
  </main>
}
