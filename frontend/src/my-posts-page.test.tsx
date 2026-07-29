import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api'
import { MyPostsPage } from './pages/MyPostsPage'
import type { PagedPosts, Post } from './types'

function post(overrides: Partial<Post>): Post {
  return {
    public_id: 'post-1', title: 'A story', slug: 'a-story',
    content_markdown: 'body', content_html: '<p>body</p>',
    status: 'published', visibility: 'public', content_version: 1,
    created_at: '2026-07-20T10:00:00Z', updated_at: '2026-07-21T10:00:00Z',
    ...overrides,
  }
}

function paged(posts: Post[], total = posts.length): PagedPosts {
  return { posts, pagination: { page: 1, page_size: 20, total, total_pages: Math.ceil(total / 20) } }
}

function renderPage() {
  return render(<MemoryRouter><MyPostsPage /></MemoryRouter>)
}

describe('MyPostsPage', () => {
  // Vitest runs without global injection here, so Testing Library's automatic
  // per-test cleanup is not registered for us.
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('lists every own post and only links View for public published ones', async () => {
    vi.spyOn(api, 'listMyPosts').mockResolvedValue(paged([
      post({ public_id: 'p1', title: 'Public story', slug: 'public-story' }),
      post({ public_id: 'p2', title: 'Secret draft', slug: 'secret-draft', status: 'draft' }),
    ]))

    renderPage()

    await waitFor(() => expect(screen.getByText('Public story')).toBeInTheDocument())
    expect(screen.getByText('Secret draft')).toBeInTheDocument()
    const editLinks = screen.getAllByRole('link', { name: 'Edit' })
    expect(editLinks[0]).toHaveAttribute('href', '/write?edit=public-story')
    expect(screen.getAllByRole('link', { name: 'View' })).toHaveLength(1)
    expect(screen.getByRole('link', { name: 'View' })).toHaveAttribute('href', '/posts/public-story')
  })

  it('reloads with the chosen status filter from page one', async () => {
    const listMine = vi.spyOn(api, 'listMyPosts').mockResolvedValue(paged([post({})]))

    renderPage()
    await waitFor(() => expect(listMine).toHaveBeenCalledWith(1, ''))

    await userEvent.click(screen.getByRole('button', { name: 'Drafts' }))
    await waitFor(() => expect(listMine).toHaveBeenCalledWith(1, 'draft'))
  })

  it('offers the editor from the empty state', async () => {
    vi.spyOn(api, 'listMyPosts').mockResolvedValue(paged([]))

    renderPage()

    await waitFor(() => expect(screen.getByText('Nothing here yet')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'Start one now.' })).toHaveAttribute('href', '/write')
  })

  it('surfaces the API error with a retry', async () => {
    vi.spyOn(api, 'listMyPosts').mockRejectedValue(new ApiError(500, {
      success: false, request_id: 'test',
      error: { code: 'internal', message: 'something broke' },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('something broke'))
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})
