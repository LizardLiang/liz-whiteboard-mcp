// playwright.config.ts
// End-to-end config for the canvas MCP suite (e2e/canvas-mcp.spec.ts).
//
// Targets the ALREADY-RUNNING isolated compose stack, on purpose. It does not
// declare a `webServer`: the stack is three containers behind Caddy, needs an
// image rebuild when either repo changes, and a Playwright-managed boot would
// turn "the stack is broken" into an opaque 120s timeout. global-setup.ts
// probes the origin and fails with the exact command to run instead.
//
// Bring the stack up first (from this directory):
//   PUBLIC_ORIGIN=http://localhost:18080 \
//     docker compose -p lizverify -f docker-compose.yml -f docker-compose.e2e.yml up -d --build
//
// Then:  bun run test:e2e
//
// fullyParallel is off and workers is 1: every test in this suite mutates the
// same seeded canvas board, and the live-render test asserts on a scene it must
// be the only writer of.
import { defineConfig, devices } from '@playwright/test'
import { ORIGIN } from './e2e/fixtures'

export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  // Generous: several tests wait on a Socket.IO broadcast reaching a browser,
  // and the OAuth mint drives a real login form per bearer.
  timeout: 120_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: [['list']],
  use: {
    baseURL: ORIGIN,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    viewport: { width: 1600, height: 1000 },
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
})
