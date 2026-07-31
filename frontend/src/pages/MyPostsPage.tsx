import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { api, ApiError } from '../api'
import { EmptyState, ErrorState } from '../components'
import type { PagedPosts, Post } from '../types'

const statusLabels: Record<string, string> = { draft: '草稿', published: '已发布', archived: '已归档' }
const visibilityLabels: Record<string, string> = { public: '公开', private: '私密' }

const filters = [
  { value: '', label: '全部' },
  { value: 'draft', label: '草稿' },
  { value: 'published', label: '已发布' },
  { value: 'archived', label: '已归档' },
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
        if (!cancelled) setError(caught instanceof ApiError ? caught.message : '无法加载你的文章。')
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
        <div><span className="eyebrow">你的创作台</span><h2>我的文章</h2></div>
        {result && <span className="muted">共 {result.pagination.total} 篇</span>}
      </div>

      <div className="ask-actions" role="group" aria-label="按状态筛选">
        {filters.map((filter) => (
          <button key={filter.value} type="button"
            className={status === filter.value ? 'button compact' : 'button ghost compact'}
            aria-pressed={status === filter.value}
            onClick={() => applyFilter(filter.value)}>{filter.label}</button>
        ))}
      </div>

      {error && <ErrorState message={error} retry={() => applyFilter(status)} />}
      {!error && loading && !result && <p className="muted">正在加载你的文章…</p>}
      {!error && result && result.posts.length === 0 && (
        <EmptyState title="还没有内容">
          你写的文章会按所有状态显示在这里。<Link to="/write">现在开始写一篇。</Link>
        </EmptyState>
      )}
      {!error && result && result.posts.length > 0 && (
        <div className="taxonomy-list">
          {result.posts.map((post) => {
            const readable = publicPath(post)
            return <div className="taxonomy-row" key={post.public_id}>
              <div>
                <strong>{post.title}</strong>
                <span>{statusLabels[post.status] || post.status} · {visibilityLabels[post.visibility] || post.visibility} · 编辑于 {new Date(post.updated_at).toLocaleDateString('zh-CN')}</span>
                {post.summary && <p>{post.summary}</p>}
              </div>
              <div>
                <Link className="text-button" to={`/write?edit=${encodeURIComponent(post.slug)}`}>编辑</Link>
                {readable && <Link className="text-button" to={readable}>查看</Link>}
              </div>
            </div>
          })}
        </div>
      )}

      {result && result.pagination.total_pages > 1 && (
        <nav className="pagination" aria-label="工作区分页">
          <button disabled={page <= 1} onClick={() => setPage(page - 1)}>← 较新</button>
          <span>{page} / {result.pagination.total_pages}</span>
          <button disabled={page >= result.pagination.total_pages} onClick={() => setPage(page + 1)}>较旧 →</button>
        </nav>
      )}
    </section>
  </main>
}
