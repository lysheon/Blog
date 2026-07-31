import { useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { useAuth } from '../auth'

export function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const { login, register } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault(); setError(''); setBusy(true)
    try {
      if (mode === 'login') await login(email, password)
      else await register(email, username, password)
      const destination = (location.state as { from?: string } | null)?.from || '/'
      navigate(destination, { replace: true })
    } catch (err) { setError(err instanceof Error ? err.message : '认证失败') }
    finally { setBusy(false) }
  }

  const loginMode = mode === 'login'
  return <main className="auth-page">
    <section className="auth-panel">
      <Link className="brand auth-brand" to="/"><span className="brand-mark">B</span><span>Blog<span className="brand-dot">.</span></span></Link>
      <div className="eyebrow">{loginMode ? '欢迎回来' : '创建你的账户'}</div>
      <h1>{loginMode ? '回到你的阅读。' : '开始书写你的故事。'}</h1>
      <p>{loginMode ? '登录后写作、评论，并从上次停下的地方继续。' : '加入一个围绕清晰观点与真诚交流的小社区。'}</p>
      {error && <div className="form-error" role="alert">{error}</div>}
      <form className="form-stack" onSubmit={submit}>
        <label>邮箱<input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required /></label>
        {!loginMode && <label>用户名<input minLength={3} maxLength={32} autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required /></label>}
        <label>密码<input type="password" minLength={8} autoComplete={loginMode ? 'current-password' : 'new-password'} value={password} onChange={(e) => setPassword(e.target.value)} required /></label>
        <button className="button full" disabled={busy}>{busy ? '请稍候…' : loginMode ? '登录' : '创建账户'}</button>
      </form>
      <p className="auth-switch">{loginMode ? '新用户？' : '已有账户？'} <Link to={loginMode ? '/register' : '/login'} state={location.state}>{loginMode ? '创建账户' : '登录'}</Link></p>
    </section>
    <aside className="auth-aside"><blockquote>“作家的职责不是说出我们都能说的话，而是说出我们无法言说之事。”</blockquote><span>— 阿娜伊斯·宁</span></aside>
  </main>
}
