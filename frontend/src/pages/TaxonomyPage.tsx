import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components'
import type { Taxonomy } from '../types'

function TaxonomyPanel({ kind, items, reload }: { kind: 'category' | 'tag'; items: Taxonomy[]; reload: () => void }) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [busy, setBusy] = useState(false)
  const kindLabel = kind === 'category' ? '分类' : '标签'

  async function create(event: FormEvent) {
    event.preventDefault(); setBusy(true)
    try {
      if (kind === 'category') await api.createCategory({ name, description: description || undefined })
      else await api.createTag({ name })
      setName(''); setDescription(''); reload()
    } finally { setBusy(false) }
  }

  async function rename(item: Taxonomy) {
    const next = window.prompt(`重命名 ${item.name}`, item.name)
    if (!next || next === item.name) return
    if (kind === 'category') await api.updateCategory(item.slug, { name: next, description: item.description })
    else await api.updateTag(item.slug, { name: next })
    reload()
  }

  async function remove(item: Taxonomy) {
    if (!window.confirm(`删除 ${item.name}？`)) return
    if (kind === 'category') await api.deleteCategory(item.slug)
    else await api.deleteTag(item.slug)
    reload()
  }

  return <section className="taxonomy-panel">
    <div className="panel-heading"><div><span className="eyebrow">管理</span><h2>{kindLabel}</h2></div><span>{items.length}</span></div>
    <form className="taxonomy-create" onSubmit={create}><input value={name} onChange={(e) => setName(e.target.value)} placeholder={`新${kindLabel}名称`} required maxLength={kind === 'category' ? 64 : 32} />{kind === 'category' && <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="可选描述" maxLength={500} />}<button className="button compact" disabled={busy}>添加</button></form>
    {items.length === 0 ? <EmptyState title={`还没有${kindLabel}`}>在上方创建第一个。</EmptyState> : <div className="taxonomy-list">{items.map((item) => <div className="taxonomy-row" key={item.public_id}><div><strong>{item.name}</strong><span>/{item.slug}</span>{item.description && <p>{item.description}</p>}</div><div><button onClick={() => void rename(item)}>重命名</button><button className="danger-text" onClick={() => void remove(item)}>删除</button></div></div>)}</div>}
  </section>
}

export function TaxonomyPage() {
  const [categories, setCategories] = useState<Taxonomy[]>([])
  const [tags, setTags] = useState<Taxonomy[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const load = useCallback(async () => {
    setError('')
    try { const [a, b] = await Promise.all([api.listCategories(), api.listTags()]); setCategories(a.categories); setTags(b.tags) }
    catch (err) { setError(err instanceof Error ? err.message : '无法加载分类信息') }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  return <main className="page-shell admin-page"><div className="editor-heading"><div><span className="eyebrow">管理后台</span><h1>整理你的内容库。</h1></div></div>{loading && <LoadingState />}{error && <ErrorState message={error} retry={() => void load()} />}{!loading && !error && <div className="taxonomy-grid"><TaxonomyPanel kind="category" items={categories} reload={() => void load()} /><TaxonomyPanel kind="tag" items={tags} reload={() => void load()} /></div>}</main>
}
