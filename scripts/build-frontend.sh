#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REDESIGN="$ROOT/job-portal-redesign(1)"
FRONTEND="$ROOT/frontend"
BACKUP="$ROOT/frontend-legacy"

cd "$REDESIGN"

PNPM="${PNPM:-pnpm}"
if ! command -v "$PNPM" >/dev/null 2>&1 && [ -x /tmp/pnpm ]; then
  PNPM=/tmp/pnpm
fi

if command -v "$PNPM" >/dev/null 2>&1; then
  "$PNPM" install
  "$PNPM" build
elif command -v npm >/dev/null 2>&1; then
  npm install
  npm run build
else
  echo "Install pnpm or npm to build the frontend." >&2
  exit 1
fi

if [ -d "$BACKUP" ]; then
  rm -rf "$BACKUP"
fi
if [ -d "$FRONTEND" ]; then
  mv "$FRONTEND" "$BACKUP"
fi

mkdir -p "$FRONTEND"
cp -a "$REDESIGN/out/." "$FRONTEND/"

# Static export uses /company/index.html; Go serves /company.html for SPA routes
if [ -f "$FRONTEND/company/index.html" ]; then
  cp "$FRONTEND/company/index.html" "$FRONTEND/company.html"
fi

echo "Frontend built to $FRONTEND (legacy saved to $BACKUP if it existed)."
