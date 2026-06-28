#!/usr/bin/env bash
# Bring up the full liz-whiteboard stack behind one origin (http://localhost:8080).
# Provisions a persistent RS256 signing key (mounted into the app) and an MCP
# client secret (written to .env, auto-loaded by compose) if absent.
# Pass extra args through (e.g. -d).
set -euo pipefail
cd "$(dirname "$0")/.."

KEYFILE="deploy/oauth-private.pem"
if [[ ! -f "$KEYFILE" ]]; then
  echo "[run] generating persistent RS256 signing key → $KEYFILE"
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$KEYFILE" 2>/dev/null
fi

# .env is auto-loaded by docker compose for ${VAR} interpolation.
if ! grep -q '^MCP_CLIENT_SECRET=' .env 2>/dev/null; then
  {
    echo "MCP_CLIENT_SECRET=$(openssl rand -hex 24)"
    echo "OAUTH_SIGNING_KEY_KID=as-key"
  } >> .env
  echo "[run] wrote MCP_CLIENT_SECRET + OAUTH_SIGNING_KEY_KID to .env"
fi

echo "[run] stack → http://localhost:8080   (app at /, MCP at /mcp)"
exec docker compose up --build "$@"
