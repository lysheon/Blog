import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router'
import { api } from '../api'
import { useAuth } from '../auth'
import { EmptyState, ErrorState, LoadingState } from '../components'
import type { Comment, PagedComments, Post } from '../types'

function CommentItem({ comment, onReply, onChanged, reply = false }: { comment: Comment; onReply: (id: string, name: string) => void; onChanged: () => void; reply?: boolean }) {
  const { user } = useAuth()
  const owner = user && (user.role === 'admin' || user.public_id === comment.author?.public_id)
  async function remove() { if (window.confirm('删除这条评论？')) { await api.deleteComment(comment.public_id); onChanged() } }
  async function edit() {
    const body = window.prompt('编辑你的评论', comment.body_markdown)
    if (!body || body === comment.body_markdown) return
    await api.updateComment(comment.public_id, body)
    onChanged()
  }
  return <article className="comment">
    <div className="comment-head"><span className="avatar small">{comment.author?.username?.[0]?.toUpperCase() || '?'}</span><strong>{comment.author?.username || '读者'}</strong><time>{new Date(comment.created_at).toLocaleDateString('zh-CN')}</time></div>
    <div className="comment-body" dangerouslySetInnerHTML={{ __html: comment.body_html }} />
    <div className="comment-actions">{!reply && <button onClick={() => onReply(comment.public_id, comment.author?.username || '读者')}>回复</button>}{owner && <button onClick={() => void edit()}>编辑</button>}{owner && <button onClick={() => void remove()}>删除</button>}</div>
    {comment.children?.map((child) => <div className="comment-reply" key={child.public_id}><CommentItem comment={child} onReply={onReply} onChanged={onChanged} reply /></div>)}
  </article>
}

export function PostPage() {
  const { slug = '' } = useParams()
  const { user } = useAuth()
  const [post, setPost] = useState<Post | null>(null)
  const [comments, setComments] = useState<PagedComments | null>(null)
  const [commentPage, setCommentPage] = useState(1)
  const [commentsError, setCommentsError] = useState('')
  const [error, setError] = useState('')
  const [body, setBody] = useState('')
  const [reply, setReply] = useState<{ id: string; name: string } | null>(null)
  const [notice, setNotice] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      const postResult = await api.getPost(slug)
      setPost(postResult.post)
      setCommentsError('')
      try { setComments(await api.listComments(slug, commentPage)) }
      catch (err) {
        setComments(null)
        setCommentsError(err instanceof Error ? err.message : '无法加载评论')
      }
    } catch (err) { setError(err instanceof Error ? err.message : '无法加载文章') }
  }, [slug, commentPage])

  useEffect(() => { void load() }, [load])

  async function submit(event: FormEvent) {
    event.preventDefault(); setNotice(''); setSubmitting(true)
    try {
      const result = await api.createComment(slug, { body_markdown: body, parent_id: reply?.id })
      setBody(''); setReply(null)
      setNotice(result.comment.status === 'pending' ? '评论已提交，等待自动审核。' : '评论已发布。')
      window.setTimeout(() => void load(), 2500)
    } catch (err) { setNotice(err instanceof Error ? err.message : '无法提交评论') }
    finally { setSubmitting(false) }
  }

  if (error) return <main className="page-shell"><ErrorState message={error} retry={() => void load()} /></main>
  if (!post) return <main className="page-shell"><LoadingState label="正在打开文章…" /></main>

  return <main>
    <article className="article-shell">
      <Link className="back-link" to="/">← 全部文章</Link>
      <header className="article-header">
        <div className="post-meta"><span>{post.categories?.[0]?.name || '随笔'}</span><time>{new Date(post.published_at || post.created_at).toLocaleDateString('zh-CN')}</time></div>
        <h1>{post.title}</h1>
        {post.summary && <p className="article-deck">{post.summary}</p>}
        <div className="article-byline"><span className="avatar">{post.author?.username?.[0]?.toUpperCase() || 'B'}</span><div><strong>{post.author?.username || '博主'}</strong><span>{post.tags?.map((tag) => tag.name).join(' · ') || '独立作者'}</span></div></div>
        {user && (user.role === 'admin' || user.public_id === post.author?.public_id) && <Link className="button ghost compact edit-story" to={`/write?edit=${encodeURIComponent(post.slug)}`}>编辑文章</Link>}
      </header>
      {post.cover_url && <img className="article-cover" src={post.cover_url} alt="" />}
      <div className="article-content" dangerouslySetInnerHTML={{ __html: post.content_html }} />
    </article>
    <section className="comments-section">
      <div className="article-shell narrow">
        <div className="section-heading"><div><span className="eyebrow">讨论</span><h2>评论</h2></div><span>{comments?.pagination.total || 0}</span></div>
        {notice && <p className="notice" role="status">{notice}</p>}
        {user && post.status === 'published' && post.visibility === 'public' ? <form className="comment-form" onSubmit={submit}>
          {reply && <div className="reply-banner">正在回复 {reply.name}<button type="button" onClick={() => setReply(null)}>取消</button></div>}
          <textarea value={body} onChange={(e) => setBody(e.target.value)} placeholder="参与讨论…支持 Markdown。" required maxLength={5000} />
          <button className="button" type="submit" disabled={submitting}>{submitting ? '发布中…' : '发表评论'}</button>
        </form> : !user ? <p className="signin-callout"><Link to="/login" state={{ from: `/posts/${slug}` }}>登录</Link>后参与讨论。</p> : <p className="signin-callout">文章公开发布后开放评论。</p>}
        {commentsError && <ErrorState message={commentsError} retry={() => void load()} />}
        {comments?.comments.length === 0 && <EmptyState title="开始讨论">成为第一个留下真诚回应的人。</EmptyState>}
        <div className="comment-list">{comments?.comments.map((comment) => <CommentItem key={comment.public_id} comment={comment} onReply={(id, name) => setReply({ id, name })} onChanged={() => void load()} />)}</div>
        {comments && comments.pagination.total_pages > 1 && <nav className="pagination" aria-label="评论分页"><button disabled={commentPage <= 1} onClick={() => setCommentPage((page) => page - 1)}>← 较早</button><span>{commentPage} / {comments.pagination.total_pages}</span><button disabled={commentPage >= comments.pagination.total_pages} onClick={() => setCommentPage((page) => page + 1)}>较新 →</button></nav>}
      </div>
    </section>
  </main>
}
