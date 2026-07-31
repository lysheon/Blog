import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Link, Navigate, Outlet, useLocation } from 'react-router'
import { useAuth } from './auth'

export function LoadingState({ label = '加载中…' }: { label?: string }) {
  return <div className="state-card" role="status"><span className="spinner" />{label}</div>
}

export function EmptyState({ title, children }: { title: string; children: ReactNode }) {
  return <div className="state-card"><strong>{title}</strong><p>{children}</p></div>
}

export function ErrorState({ message, retry }: { message: string; retry?: () => void }) {
  return <div className="state-card error-state" role="alert"><strong>出错了</strong><p>{message}</p>{retry && <button className="button ghost" onClick={retry}>重试</button>}</div>
}

export function ProtectedRoute({ admin = false }: { admin?: boolean }) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return <LoadingState label="正在恢复会话…" />
  if (!user) return <Navigate to="/login" replace state={{ from: `${location.pathname}${location.search}${location.hash}` }} />
  if (admin && user.role !== 'admin') return <Navigate to="/" replace />
  return <Outlet />
}

interface BoundaryState { error: Error | null }
export class ErrorBoundary extends Component<{ children: ReactNode }, BoundaryState> {
  state: BoundaryState = { error: null }
  static getDerivedStateFromError(error: Error): BoundaryState { return { error } }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error('Uncaught frontend error', error, info) }
  render() {
    if (this.state.error) {
      return <main className="page-shell"><ErrorState message={this.state.error.message} /><Link className="text-link" to="/">返回首页</Link></main>
    }
    return this.props.children
  }
}

export function ConfirmButton({ children, onConfirm, label = '确认执行此操作？' }: { children: ReactNode; onConfirm: () => void | Promise<void>; label?: string }) {
  return <button className="button danger" onClick={() => { if (window.confirm(label)) void onConfirm() }}>{children}</button>
}
