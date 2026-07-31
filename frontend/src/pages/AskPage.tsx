import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router'
import { api, ApiError } from '../api'
import type { AIAnswer } from '../types'

export function AskPage() {
  const [question, setQuestion] = useState('')
  const [result, setResult] = useState<AIAnswer | null>(null)
  const [error, setError] = useState('')
  const [unavailable, setUnavailable] = useState(false)
  const [loading, setLoading] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    const value = question.trim()
    if (!value || loading) return
    setLoading(true)
    setError('')
    setResult(null)
    try {
      setResult(await api.askAI(value))
    } catch (caught) {
      // A deployment can run without AI. That is a configuration state, not a
      // failed request, so it gets its own explanation instead of an error.
      if (caught instanceof ApiError && caught.code === 'ai_not_enabled') {
        setUnavailable(true)
      } else {
        setError(caught instanceof ApiError ? caught.message : '文章助手暂时不可用。')
      }
    } finally {
      setLoading(false)
    }
  }

  return <main className="page-shell ask-page">
    <section className="ask-hero">
      <div className="eyebrow">文章助手</div>
      <h1>向已发布文章提问。</h1>
      <p>回答基于公开文章生成，并附带可溯源链接。</p>
      <form className="ask-form" onSubmit={(event) => void submit(event)}>
        <label htmlFor="rag-question">你的问题</label>
        <textarea id="rag-question" value={question} maxLength={2000} rows={5} disabled={unavailable}
          onChange={(event) => setQuestion(event.target.value)}
          placeholder="这些文章提到了什么关于…？" />
        <div className="ask-actions"><span>{question.length} / 2000</span><button className="button" disabled={loading || unavailable || !question.trim()}>{loading ? '检索中…' : '提问'}</button></div>
      </form>
    </section>

    {unavailable && <section className="state-card" role="status">
      <strong>文章助手未启用</strong>
      <p>当前部署未启用 AI 问答。所有已发布文章仍可正常阅读——<Link to="/">浏览文章列表</Link>。</p>
    </section>}
    {error && <section className="error-state" role="alert"><strong>提问失败</strong><p>{error}</p></section>}
    {result && <section className="answer-panel" aria-live="polite">
      <div className="eyebrow">基于文章的答案</div>
      <p className="answer-text">{result.answer}</p>
      <h2>来源</h2>
      {result.sources.length === 0 ? <p className="muted">未找到足够相关的已发布来源。</p> :
        <div className="source-list">{result.sources.map((source) => <Link className="source-card" key={source.post_id} to={`/posts/${source.slug}`}>
          <div><strong>{source.title}</strong><span>相似度 {Math.round(source.score * 100)}%</span></div>
          <p>{source.excerpt}</p>
        </Link>)}</div>}
    </section>}
  </main>
}
