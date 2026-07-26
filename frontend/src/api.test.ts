import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, onAuthenticationLost, refreshAccessToken, setAccessToken } from './api'

type FetchCall = { url: string; init: RequestInit }

let calls: FetchCall[] = []

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function success(data: unknown) {
  return { success: true, data }
}

function failure(code: string, message: string) {
  return { success: false, request_id: 'test', error: { code, message } }
}

/** Routes each request to the next queued responder for that path. */
function mockFetch(routes: Record<string, Array<() => Response>>) {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = String(input)
    calls.push({ url, init })
    const path = Object.keys(routes).find((candidate) => url.includes(candidate))
    const queue = path ? routes[path] : undefined
    if (!queue || queue.length === 0) throw new Error(`unexpected request: ${url}`)
    return Promise.resolve(queue.length === 1 ? queue[0]() : queue.shift()!())
  }))
}

function pathsCalled() {
  return calls.map((call) => `${call.init.method ?? 'GET'} ${call.url.replace('/api/v1', '')}`)
}

describe('api session handling', () => {
  beforeEach(() => {
    calls = []
    setAccessToken(null)
    onAuthenticationLost(null)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    setAccessToken(null)
    onAuthenticationLost(null)
  })

  it('refreshes proactively before a near-expiry token is used', async () => {
    setAccessToken('about-to-expire', 3)
    mockFetch({
      '/auth/refresh': [() => jsonResponse(200, success({ access_token: 'fresh', expires_in: 900, token_type: 'Bearer' }))],
      '/posts': [() => jsonResponse(200, success({ posts: [], pagination: { page: 1, page_size: 9, total: 0, total_pages: 0 } }))],
    })

    await api.listPosts()

    expect(pathsCalled()).toEqual(['POST /auth/refresh', 'GET /posts?page=1&page_size=9'])
    expect(calls[1].init.headers).toBeInstanceOf(Headers)
    expect((calls[1].init.headers as Headers).get('Authorization')).toBe('Bearer fresh')
  })

  it('replays a 401 exactly once after refreshing', async () => {
    setAccessToken('stale', 900)
    mockFetch({
      '/posts': [
        () => jsonResponse(401, failure('unauthorized', 'expired')),
        () => jsonResponse(200, success({ posts: [], pagination: { page: 1, page_size: 9, total: 0, total_pages: 0 } })),
      ],
      '/auth/refresh': [() => jsonResponse(200, success({ access_token: 'fresh', expires_in: 900, token_type: 'Bearer' }))],
    })

    await api.listPosts()

    expect(pathsCalled()).toEqual([
      'GET /posts?page=1&page_size=9',
      'POST /auth/refresh',
      'GET /posts?page=1&page_size=9',
    ])
  })

  it('gives up instead of looping when the refresh itself fails', async () => {
    setAccessToken('stale', 900)
    const lost = vi.fn()
    onAuthenticationLost(lost)
    mockFetch({
      '/posts': [() => jsonResponse(401, failure('unauthorized', 'expired'))],
      '/auth/refresh': [() => jsonResponse(401, failure('unauthorized', 'refresh token not found'))],
    })

    await expect(api.listPosts()).rejects.toMatchObject({ status: 401 })

    expect(pathsCalled()).toEqual(['GET /posts?page=1&page_size=9', 'POST /auth/refresh'])
    expect(lost).toHaveBeenCalledTimes(1)
  })

  it('shares one refresh across concurrent callers', async () => {
    let refreshes = 0
    mockFetch({
      '/auth/refresh': [() => {
        refreshes += 1
        return jsonResponse(200, success({ access_token: 'fresh', expires_in: 900, token_type: 'Bearer' }))
      }],
    })

    const results = await Promise.all([refreshAccessToken(), refreshAccessToken(), refreshAccessToken()])

    expect(results).toEqual([true, true, true])
    expect(refreshes).toBe(1)
  })

  it('does not refresh on behalf of the sign-in endpoints', async () => {
    setAccessToken('about-to-expire', 3)
    mockFetch({
      '/auth/login': [() => jsonResponse(401, failure('unauthorized', 'invalid email or password'))],
    })

    await expect(api.login({ email: 'a@example.test', password: 'wrong' })).rejects.toMatchObject({ status: 401 })

    expect(pathsCalled()).toEqual(['POST /auth/login'])
  })
})
