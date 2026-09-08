// e2e/global-setup.ts
// Runs once before the canvas MCP suite.
//
//   1. Resets the fixture boards by running the app image's own seed scripts
//      inside the app container.
//   2. Logs the EDITOR user in through the real /login form and saves the
//      session cookie, so the live-render test can open the board as a real
//      authenticated viewer without repeating the login.
//
// It does NOT start the stack. Bringing Docker up from inside a Playwright
// global setup would hide a broken stack behind a test timeout; the suite is
// meant to fail immediately and legibly when the origin is not there.
import { mkdirSync } from 'node:fs'
import { chromium } from '@playwright/test'
import { EDITOR_USER, ORIGIN } from './fixtures'
import { seedCanvasFixtures } from './stack'

export const EDITOR_STORAGE_STATE = 'e2e/.auth/editor.json'

export default async function globalSetup() {
  // Fail fast and say why, rather than letting every test time out.
  try {
    const probe = await fetch(`${ORIGIN}/.well-known/oauth-protected-resource`)
    if (!probe.ok) throw new Error(`HTTP ${probe.status}`)
  } catch (cause) {
    throw new Error(
      `The stack is not answering at ${ORIGIN}. Bring it up first:\n` +
        `  PUBLIC_ORIGIN=${ORIGIN} docker compose -p lizverify ` +
        `-f docker-compose.yml -f docker-compose.e2e.yml up -d --build\n` +
        `cause: ${String(cause)}`,
    )
  }

  seedCanvasFixtures()

  mkdirSync('e2e/.auth', { recursive: true })
  const browser = await chromium.launch()
  try {
    const context = await browser.newContext()
    const page = await context.newPage()
    await page.goto(`${ORIGIN}/login`)
    await page.waitForLoadState('networkidle')
    const email = page.getByRole('textbox', { name: 'Email' })
    const password = page.getByRole('textbox', { name: 'Password' })
    await email.click()
    await email.pressSequentially(EDITOR_USER.email)
    await password.click()
    await password.pressSequentially(EDITOR_USER.password)
    await page.getByRole('button', { name: 'Sign in' }).click()

    const deadline = Date.now() + 20_000
    let authenticated = false
    while (Date.now() < deadline) {
      const cookies = await context.cookies()
      if (cookies.some((c) => c.name === 'session_token' && c.value)) {
        authenticated = true
        break
      }
      await new Promise((resolve) => setTimeout(resolve, 200))
    }
    if (!authenticated) throw new Error('editor login did not set session_token')

    await context.storageState({ path: EDITOR_STORAGE_STATE })
  } finally {
    await browser.close()
  }
}
