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
