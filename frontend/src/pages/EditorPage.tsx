import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { api } from '../api'
import { ErrorState, LoadingState } from '../components'
import type { Post, PostInput, Taxonomy } from '../types'

const blank: PostInput = { title: '', content_markdown: '', summary: '', cover_url: '', status: 'draft', visibility: 'public', category_ids: [], tag_ids: [] }

export function EditorPage() {
  const [params] = useSearchParams()
  const editSlug = params.get('edit')
  const navigate = useNavigate()
  const [form, setForm] = useState<PostInput>(blank)
  const [categories, setCategories] = useState<Taxonomy[]>([])
  const [tags, setTags] = useState<Taxonomy[]>([])
  const [loadedPost, setLoadedPost] = useState<Post | null>(null)
  const [loading, setLoading] = useState(Boolean(editSlug))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    setError('')
    setLoading(Boolean(editSlug))
    if (!editSlug) { setLoadedPost(null); setForm(blank) }
    void Promise.all([api.listCategories(), api.listTags()]).then(([cats, tagResult]) => {
      if (active) { setCategories(cats.categories); setTags(tagResult.tags) }
    }).catch((err) => { if (active) setError(err instanceof Error ? err.message : '无法加载分类信息') })
    if (editSlug) {
      void api.getPost(editSlug).then(({ post }) => {
        if (!active) return
        setLoadedPost(post)
        setForm({ title: post.title, content_markdown: post.content_markdown, summary: post.summary || '', cover_url: post.cover_url || '', status: post.status, visibility: post.visibility, category_ids: post.categories?.map((x) => x.public_id) || [], tag_ids: post.tags?.map((x) => x.public_id) || [] })
      }).catch((err) => { if (active) setError(err instanceof Error ? err.message : '无法加载文章') }).finally(() => { if (active) setLoading(false) })
    }
    return () => { active = false }
  }, [editSlug])

  function toggle(field: 'category_ids' | 'tag_ids', id: string) {
    setForm((current) => ({ ...current, [field]: current[field].includes(id) ? current[field].filter((x) => x !== id) : [...current[field], id] }))
  }

  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError('')
    try {
      const result = editSlug ? await api.updatePost(editSlug, form) : await api.createPost(form)
      navigate(`/posts/${result.post.slug}`)
    } catch (err) { setError(err instanceof Error ? err.message : '无法保存文章') }
    finally { setBusy(false) }
  }

  async function remove() {
    if (!editSlug || !window.confirm('删除这篇文章？此操作无法撤销。')) return
    await api.deletePost(editSlug); navigate('/')
  }

  if (loading) return <main className="page-shell"><LoadingState label="正在打开编辑器…" /></main>
  return <main className="editor-page page-shell">
    <div className="editor-heading"><div><span className="eyebrow">{editSlug ? '修改文章' : '新草稿'}</span><h1>{editSlug ? loadedPost?.title : '写下值得留存的内容。'}</h1></div>{editSlug && <button className="button danger" onClick={() => void remove()}>删除文章</button>}</div>
    {error && <ErrorState message={error} />}
    <form className="editor-form" onSubmit={submit}>
      <div className="editor-main">
        <label>标题<input className="title-input" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} maxLength={200} placeholder="一个清晰难忘的标题" required /></label>
        <label>摘要<textarea className="summary-input" value={form.summary} onChange={(e) => setForm({ ...form, summary: e.target.value })} maxLength={500} placeholder="一段引人阅读的简介…" /></label>
        <label>正文 <span>Markdown</span><textarea className="content-input" value={form.content_markdown} onChange={(e) => setForm({ ...form, content_markdown: e.target.value })} placeholder="从一个挥之不去的想法开始…" required /></label>
      </div>
      <aside className="editor-sidebar">
        <label>状态<select value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value as PostInput['status'] })}><option value="draft">草稿</option><option value="published">已发布</option><option value="archived">已归档</option></select></label>
        <label>可见性<select value={form.visibility} onChange={(e) => setForm({ ...form, visibility: e.target.value as PostInput['visibility'] })}><option value="public">公开</option><option value="private">私密</option></select></label>
        <label>封面图片 URL<input type="url" value={form.cover_url} onChange={(e) => setForm({ ...form, cover_url: e.target.value })} placeholder="https://…" /></label>
        <fieldset><legend>分类</legend>{categories.map((item) => <label className="check-row" key={item.public_id}><input type="checkbox" checked={form.category_ids.includes(item.public_id)} onChange={() => toggle('category_ids', item.public_id)} />{item.name}</label>)}</fieldset>
        <fieldset><legend>标签</legend>{tags.map((item) => <label className="check-row" key={item.public_id}><input type="checkbox" checked={form.tag_ids.includes(item.public_id)} onChange={() => toggle('tag_ids', item.public_id)} />{item.name}</label>)}</fieldset>
        <button className="button full" disabled={busy}>{busy ? '保存中…' : editSlug ? '保存修改' : form.status === 'published' ? '发布文章' : '保存草稿'}</button>
      </aside>
    </form>
  </main>
}
