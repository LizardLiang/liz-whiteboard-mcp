// e2e/global-setup.ts
// Runs once before the canvas MCP suite.
//
//   1. Resets the fixture boards by running the app image's own seed scripts
//      inside the app container.
//   2. Logs the EDITOR and the VIEWER user in through the real /login form,
//      once each, and saves each session cookie as a storageState file. Every
//      spec mints its bearers from these files (e2e/oauth.ts) and opens its
//      browser pages with them, so a full run performs exactly 2 logins.
//
// It does NOT start the stack. Bringing Docker up from inside a Playwright
// global setup would hide a broken stack behind a test timeout; the suite is
// meant to fail immediately and legibly when the origin is not there.
import { mkdirSync } from 'node:fs'
import { EDITOR_USER, ORIGIN, VIEWER_USER } from './fixtures'
import { logIn } from './oauth'
import { seedCanvasFixtures } from './stack'

export const EDITOR_STORAGE_STATE = 'e2e/.auth/editor.json'
export const VIEWER_STORAGE_STATE = 'e2e/.auth/viewer.json'

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
  await logIn(EDITOR_USER, EDITOR_STORAGE_STATE)
  await logIn(VIEWER_USER, VIEWER_STORAGE_STATE)
}
