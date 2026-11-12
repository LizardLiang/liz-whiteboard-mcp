// e2e/oauth.ts
// Mints a REAL user MCP access token through the real OAuth 2.1 flow.
//
// No stubs and no hand-signed tokens: the whole point of this suite is that the
// bearer a tool call carries is one the Authorization Server actually issued to
// a user who actually logged in. A forged token would prove the tools work
// against a verifier that was never exercised.
//
// Two steps, split so a run logs each user in ONCE:
//   1. `logIn` (global setup only) drives the real /login form and saves the
//      session_token cookie as a Playwright storageState file.
//      NODE_ENV=production in the container disables the DEBUG_SUPER_PASSWORD
//      bypass, so this must be a genuine seeded account. The app allows 30
//      logins per IP per 15 min (app src/lib/auth/auth-limits.ts); one login
//      per user per run keeps back-to-back runs far below that limit.
//   2. `mintAccessToken` (every spec) reuses that cookie:
//      a. GET /authorize with PKCE S256. `mcp-claude` is a first-party client
//         and auto-approves, so the response is a 302 straight to the
//         redirect URI.
//      b. Read the code from the Location header. The registered redirect is a
//         loopback URI; running an actual listener on :10000 would add a port
//         collision for no extra coverage, since nothing about the AS behaves
//         differently when the client is a real socket.
//      c. POST /token with the code_verifier.
//
// `resource` is the CANONICAL PUBLIC MCP URI on both calls. Sending an internal
// host (http://app:3000/mcp) mints a token whose audience the resource server
// rejects, and every later tool call 401s.
import crypto from 'node:crypto'
import { chromium, request } from '@playwright/test'
import { OAUTH_CLIENT_ID, OAUTH_REDIRECT_URI, ORIGIN, MCP_URL } from './fixtures'

function base64url(buffer: Buffer): string {
  return buffer
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
}

export interface Credentials {
  email: string
  password: string
}

/**
 * Log `user` in through the real /login form and save the session cookie to
 * `storageStatePath`. Global setup calls this once per user per run.
 */
export async function logIn(
  user: Credentials,
  storageStatePath: string,
): Promise<void> {
  const browser = await chromium.launch()
  try {
    const context = await browser.newContext()
    const page = await context.newPage()

    await page.goto(`${ORIGIN}/login`)
    await page.waitForLoadState('networkidle')
    // pressSequentially, not fill: the form is React-controlled and the app's
    // own global-setup.ts types character by character for that reason.
    const email = page.getByRole('textbox', { name: 'Email' })
    const password = page.getByRole('textbox', { name: 'Password' })
    await email.click()
    await email.pressSequentially(user.email)
    await password.click()
    await password.pressSequentially(user.password)
    await page.getByRole('button', { name: 'Sign in' }).click()

    // Wait on the HttpOnly cookie, not on a redirect: the post-login client
    // redirect can bounce back to /login (a known app quirk the app's own
    // global-setup.ts documents), but the cookie is set either way.
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
    if (!authenticated) {
      throw new Error(
        `login did not set session_token for ${user.email} — is the account ` +
          `seeded, or did this IP hit the login rate limit?`,
      )
    }

    await context.storageState({ path: storageStatePath })
  } finally {
    await browser.close()
  }
}

/**
 * Return a bearer token scoped to the MCP resource for the user whose session
 * global setup saved at `storageStatePath`. Performs no login.
 */
export async function mintAccessToken(storageStatePath: string): Promise<string> {
  const context = await request.newContext({ storageState: storageStatePath })
  try {
    const verifier = base64url(crypto.randomBytes(32))
    const challenge = base64url(
      crypto.createHash('sha256').update(verifier).digest(),
    )

    const authorizeUrl =
      `${ORIGIN}/authorize?response_type=code` +
      `&client_id=${encodeURIComponent(OAUTH_CLIENT_ID)}` +
      `&redirect_uri=${encodeURIComponent(OAUTH_REDIRECT_URI)}` +
      `&scope=whiteboard` +
      `&code_challenge=${challenge}&code_challenge_method=S256` +
      `&resource=${encodeURIComponent(MCP_URL)}` +
      `&state=canvas-mcp-e2e`

    const authorized = await context.get(authorizeUrl, { maxRedirects: 0 })
    const location = authorized.headers()['location']
    if (!location) {
      throw new Error(
        `/authorize returned ${authorized.status()} with no Location header: ` +
          (await authorized.text()).slice(0, 500),
      )
    }
    const code = new URL(location).searchParams.get('code')
    if (!code) {
      throw new Error(
        `no code in redirect: ${location} — is the session in ` +
          `${storageStatePath} still valid?`,
      )
    }

    const tokenResponse = await context.post(`${ORIGIN}/token`, {
      form: {
        grant_type: 'authorization_code',
        code,
        redirect_uri: OAUTH_REDIRECT_URI,
        client_id: OAUTH_CLIENT_ID,
        code_verifier: verifier,
        resource: MCP_URL,
      },
    })
    const body = (await tokenResponse.json()) as { access_token?: string }
    if (!body.access_token) {
      throw new Error(
        `/token returned ${tokenResponse.status()}: ${JSON.stringify(body)}`,
      )
    }
    return body.access_token
  } finally {
    await context.dispose()
  }
}
