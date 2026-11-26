#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

chmod +x .githooks/pre-commit
git config core.hooksPath .githooks

echo "Configured git core.hooksPath to .githooks (.githooks/pre-commit enabled)."

export PATH="${HOME}/.local/bin:/usr/local/bin:/opt/homebrew/bin:${PATH}"
if command -v gitleaks >/dev/null 2>&1; then
  echo "Detected gitleaks: $(gitleaks version)"
else
  echo "Warning: gitleaks is not currently in PATH. Install gitleaks v8 before committing:" >&2
  echo "  https://github.com/gitleaks/gitleaks#installing" >&2
fi
