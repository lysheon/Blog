import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components'
import type { PagedPosts } from '../types'

function formatDate(value?: string) {
  if (!value) return '草稿'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(new Date(value))
}

export function HomePage() {
  const [params, setParams] = useSearchParams()
  const requestedPage = Number(params.get('page'))
  const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1
  const [result, setResult] = useState<PagedPosts | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try { setResult(await api.listPosts(page)) }
    catch (err) { setError(err instanceof Error ? err.message : '无法加载文章') }
    finally { setLoading(false) }
  }, [page])

  useEffect(() => { void load() }, [load])

  return (
    <main>
      <section className="hero-section page-shell">
        <div className="eyebrow">独立观点 · 精心编辑</div>
        <h1>值得<br /><em>慢下来</em>阅读的故事。</h1>
        <p>关于细节与思考的随笔、笔记和实践探索。</p>
        <a className="button" href="#latest">浏览最新文章</a>
      </section>
      <section id="latest" className="page-shell section-block">
        <div className="section-heading"><div><span className="eyebrow">文章</span><h2>最新文章</h2></div>{result && <span>已发布 {result.pagination.total} 篇</span>}</div>
        {loading && <LoadingState label="正在加载文章…" />}
        {error && <ErrorState message={error} retry={() => void load()} />}
        {!loading && !error && result?.posts.length === 0 && <EmptyState title="还没有文章">第一篇发布的故事将在这里出现。</EmptyState>}
        <div className="post-grid">
          {result?.posts.map((post, index) => (
            <article className={`post-card ${index === 0 ? 'featured' : ''}`} key={post.public_id}>
              {post.cover_url ? <img src={post.cover_url} alt="" /> : <div className="cover-placeholder"><span>{String(index + 1).padStart(2, '0')}</span></div>}
              <div className="post-card-body">
                <div className="post-meta"><span>{post.categories?.[0]?.name || '随笔'}</span><time>{formatDate(post.published_at)}</time></div>
                <h3><Link to={`/posts/${post.slug}`}>{post.title}</Link></h3>
                <p>{post.summary || '打开文章阅读全文。'}</p>
                <div className="post-author"><span className="avatar">{post.author?.username?.[0]?.toUpperCase() || 'B'}</span><span>{post.author?.username || '博主'}</span></div>
              </div>
            </article>
          ))}
        </div>
        {result && result.pagination.total_pages > 1 && <nav className="pagination" aria-label="文章分页">
          <button disabled={page <= 1} onClick={() => setParams({ page: String(page - 1) })}>← 较新</button>
          <span>{page} / {result.pagination.total_pages}</span>
          <button disabled={page >= result.pagination.total_pages} onClick={() => setParams({ page: String(page + 1) })}>较旧 →</button>
        </nav>}
      </section>
    </main>
  )
}
