import { NavLink, Outlet, Link, useNavigate } from 'react-router'
import { useAuth } from './auth'

export function AppLayout() {
  const { user, profile, logout } = useAuth()
  const navigate = useNavigate()

  async function signOut() {
    try { await logout() }
    finally { navigate('/') }
  }

  return (
    <div className="app-frame">
      <header className="site-header">
        <Link className="brand" to="/" aria-label="Blog 首页">
          <span className="brand-mark">B</span>
          <span>Blog<span className="brand-dot">.</span></span>
        </Link>
        <nav className="main-nav" aria-label="主导航">
          <NavLink to="/" end>文章</NavLink>
          <NavLink to="/ask">AI 问答</NavLink>
          {user && <NavLink to="/me/posts">我的文章</NavLink>}
          {user && <NavLink to="/write">写作</NavLink>}
          {user?.role === 'admin' && <NavLink to="/admin/taxonomy">分类管理</NavLink>}
        </nav>
        <div className="account-nav">
          {user ? (
            <>
              <span className="user-chip" title={user.email}>{profile?.display_name || user.username}</span>
              <button className="text-button" onClick={() => void signOut()}>退出登录</button>
            </>
          ) : (
            <>
              <Link className="text-link" to="/login">登录</Link>
              <Link className="button compact" to="/register">注册</Link>
            </>
          )}
        </div>
      </header>
      <Outlet />
      <footer className="site-footer">
        <div><strong>Blog.</strong><span>用心写作，开放分享。</span></div>
        <span>独立写作 · 以已发布内容为根基的 AI 问答</span>
      </footer>
    </div>
  )
}
