import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api'
import { AskPage } from './pages/AskPage'

function renderAskPage() {
  return render(<MemoryRouter><AskPage /></MemoryRouter>)
}

async function ask(question: string) {
  await userEvent.type(screen.getByLabelText('Your question'), question)
  await userEvent.click(screen.getByRole('button', { name: 'Ask' }))
}

describe('AskPage', () => {
  // This project runs Vitest without global injection, so Testing Library's
  // automatic per-test cleanup is not registered for us.
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('explains that answering is switched off instead of reporting a failure', async () => {
    vi.spyOn(api, 'askAI').mockRejectedValue(new ApiError(503, {
      success: false,
      request_id: 'test',
      error: { code: 'ai_not_enabled', message: 'AI question answering is not enabled' },
    }))

    renderAskPage()
    await ask('What do these stories cover?')

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The article assistant is turned off'))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Your question')).toBeDisabled()
  })

  it('reports genuine upstream failures as errors', async () => {
    vi.spyOn(api, 'askAI').mockRejectedValue(new ApiError(503, {
      success: false,
      request_id: 'test',
      error: { code: 'ai_unavailable', message: 'AI question answering is temporarily unavailable' },
    }))

    renderAskPage()
    await ask('What do these stories cover?')

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('temporarily unavailable'))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Your question')).toBeEnabled()
  })

  it('renders a grounded answer with its linked sources', async () => {
    vi.spyOn(api, 'askAI').mockResolvedValue({
      answer: 'The stories cover deployment. [1]',
      sources: [{ post_id: 'post-1', title: 'Deployment notes', slug: 'deployment-notes', excerpt: 'How the stack starts.', score: 0.82 }],
    })

    renderAskPage()
    await ask('What do these stories cover?')

    await waitFor(() => expect(screen.getByText('The stories cover deployment. [1]')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: /Deployment notes/ })).toHaveAttribute('href', '/posts/deployment-notes')
  })
})
