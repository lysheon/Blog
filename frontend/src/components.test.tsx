import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { EmptyState } from './components'

describe('EmptyState', () => {
  it('renders an accessible empty message', () => {
    render(<MemoryRouter><EmptyState title="还没有文章">发布第一篇。</EmptyState></MemoryRouter>)
    expect(screen.getByText('还没有文章')).toBeInTheDocument()
    expect(screen.getByText('发布第一篇。')).toBeInTheDocument()
  })
})
