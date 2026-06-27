import { defineConfig } from 'vitest/config'
import viteTsConfigPaths from 'vite-tsconfig-paths'

// Separate vitest config for MCP server tests.
// Uses environment: 'node' to avoid the jsdom baseline and run pure Node.js tests.
// Run with: bun run test:mcp
export default defineConfig({
  plugins: [
    viteTsConfigPaths({
      projects: ['./tsconfig.json'],
    }),
  ],
  test: {
    environment: 'node',
    globals: false,
    include: ['src/mcp/__tests__/**/*.test.ts'],
  },
})
