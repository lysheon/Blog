import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// This suite drives the built SPA against a live Go API, MySQL, Redis and
// Worker — nothing under /api/v1 is mocked. It is the drift detector between
// the frontend contracts and the real backend.
//
// The harness (scripts/testing/run-real-api-e2e.sh) provides the stack and a
// database that may already contain rows from earlier runs, so assertions
// only reference data created inside each test.

const suffix = `${Date.now()}`
const email = `e2e-${suffix}@example.test`
const username = `e2e_${suffix}`
const password = 'safe-password-123'
const title = `Real API flow ${suffix}`
const commentBody = `A grounded comment ${suffix}`

async function expectSignedIn(page: Page) {
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
}

test('a reader can register, publish, comment and share the story', async ({ page }) => {
  await page.goto('/register')
  await page.getByRole('textbox', { name: 'Email' }).fill(email)
  await page.getByRole('textbox', { name: 'Username' }).fill(username)
  await page.getByRole('textbox', { name: 'Password' }).fill(password)
  await page.getByRole('button', { name: 'Create account' }).click()
  await expectSignedIn(page)

  // The session must survive a full reload through the real refresh cookie.
  await page.reload()
  await expectSignedIn(page)

  await page.getByRole('link', { name: 'Write' }).click()
  await page.getByRole('textbox', { name: 'Title' }).fill(title)
  await page.getByRole('textbox', { name: 'Summary' }).fill('Written by the real-API browser flow.')
  await page.getByRole('textbox', { name: 'Story Markdown' }).fill('# Grounded\n\nThis story exists in real MySQL.')
  await page.getByRole('combobox', { name: 'Status' }).selectOption('published')
  await page.getByRole('button', { name: 'Publish story' }).click()

  await expect(page.getByRole('heading', { name: title })).toBeVisible()
  const postURL = page.url()

  await page.getByRole('textbox', { name: /Add to the conversation/ }).fill(commentBody)
  await page.getByRole('button', { name: 'Post comment' }).click()
  await expect(page.getByRole('status')).toContainText('awaiting automatic moderation')

  // The Worker approves the comment asynchronously; an anonymous visitor only
  // sees it once moderation completed. Comments load after the page itself, so
  // each retry gives the fetch a short window before reloading.
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page.getByRole('link', { name: 'Sign in' })).toBeVisible()
  await expect(async () => {
    await page.goto(postURL)
    await expect(page.getByText(commentBody)).toBeVisible({ timeout: 3_000 })
  }).toPass({ timeout: 45_000 })

  // The published story is discoverable from the public list as well.
  await page.goto('/')
  await page.getByRole('link', { name: new RegExp(title) }).click()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
})

test('the ask page reports that answering is switched off', async ({ page }) => {
  // The harness runs with AI disabled, so the real backend answers
  // ai_not_enabled and the page must explain the state instead of failing.
  await page.goto('/ask')
  await page.getByRole('textbox', { name: 'Your question' }).fill('Is the assistant available?')
  await page.getByRole('button', { name: 'Ask' }).click()

  await expect(page.getByRole('status')).toContainText('The article assistant is turned off')
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: 'Your question' })).toBeDisabled()
})

test('the workspace gathers every own story for its author', async ({ page }) => {
  const ws = `${Date.now()}w`
  const draftTitle = `Workspace draft ${ws}`
  const publishedTitle = `Workspace published ${ws}`

  await page.goto('/register')
  await page.getByRole('textbox', { name: 'Email' }).fill(`e2e-${ws}@example.test`)
  await page.getByRole('textbox', { name: 'Username' }).fill(`e2e_${ws}`)
  await page.getByRole('textbox', { name: 'Password' }).fill(password)
  await page.getByRole('button', { name: 'Create account' }).click()
  await expectSignedIn(page)

  // One draft and one published story, both through the real editor.
  await page.getByRole('link', { name: 'Write' }).click()
  await page.getByRole('textbox', { name: 'Title' }).fill(draftTitle)
  await page.getByRole('textbox', { name: 'Story Markdown' }).fill('Draft body kept out of the public list.')
  await page.getByRole('button', { name: 'Save draft' }).click()
  await expect(page.getByRole('heading', { name: draftTitle })).toBeVisible()

  await page.getByRole('link', { name: 'Write' }).click()
  await page.getByRole('textbox', { name: 'Title' }).fill(publishedTitle)
  await page.getByRole('textbox', { name: 'Story Markdown' }).fill('Published body readers can open.')
  await page.getByRole('combobox', { name: 'Status' }).selectOption('published')
  await page.getByRole('button', { name: 'Publish story' }).click()
  await expect(page.getByRole('heading', { name: publishedTitle })).toBeVisible()

  await page.getByRole('link', { name: 'My stories' }).click()
  await expect(page.getByText(draftTitle)).toBeVisible()
  await expect(page.getByText(publishedTitle)).toBeVisible()

  // Only the story a reader could actually open offers a View link.
  const draftRow = page.locator('.taxonomy-row', { hasText: draftTitle })
  const publishedRow = page.locator('.taxonomy-row', { hasText: publishedTitle })
  await expect(publishedRow.getByRole('link', { name: 'View' })).toBeVisible()
  await expect(draftRow.getByRole('link', { name: 'View' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Drafts' }).click()
  await expect(page.getByText(publishedTitle)).toHaveCount(0)
  await expect(page.getByText(draftTitle)).toBeVisible()

  // Edit drops straight back into the real editor with the draft loaded.
  await draftRow.getByRole('link', { name: 'Edit' }).click()
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue(draftTitle)
})
