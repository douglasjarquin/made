#!/bin/sh
# Interim pinned installer for Cursor Cloud environments (project issue #43).
# Not release automation - see issue #27 for the real, checksummed,
# code-signed release path this replaces once it exists.
set -eu

REPO_URL="${MADE_REPO_URL:-https://github.com/douglasjarquin/made.git}"
INSTALL_DIR="${MADE_INSTALL_DIR:-$HOME/.local/bin}"
GOLANGCI_LINT_VERSION="v2.11.2"

sha="${1:-${MADE_PINNED_SHA:-}}"
if [ -z "$sha" ]; then
  if git rev-parse HEAD >/dev/null 2>&1; then
    sha="$(git rev-parse HEAD)"
  else
    echo "install-cursor-cloud.sh: no pinned SHA given; pass one as \$1 or set MADE_PINNED_SHA" >&2
    exit 2
  fi
fi

mkdir -p "$INSTALL_DIR"
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

checkout_dir="$workdir/made"
if git rev-parse --is-inside-work-tree >/dev/null 2>&1 && git cat-file -e "$sha^{commit}" 2>/dev/null; then
  local_root="$(git rev-parse --show-toplevel)"
  git -C "$local_root" worktree add --detach "$checkout_dir" "$sha" >/dev/null
  trap 'git -C "$local_root" worktree remove --force "$checkout_dir" >/dev/null 2>&1 || true; rm -rf "$workdir"' EXIT
else
  git clone --quiet "$REPO_URL" "$checkout_dir"
  git -C "$checkout_dir" checkout --quiet --detach "$sha"
fi

(
  cd "$checkout_dir"
  go build -ldflags "-X github.com/douglasjarquin/made/internal/managed.MadeVersion=$sha" -o "$INSTALL_DIR/made" ./cmd/made
)
echo "made: installed pinned build $sha to $INSTALL_DIR/made"

curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$INSTALL_DIR" "$GOLANGCI_LINT_VERSION"
echo "made: installed golangci-lint $GOLANGCI_LINT_VERSION to $INSTALL_DIR/golangci-lint"

if [ "$(uname -s)" = "Linux" ] && ! command -v bwrap >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    (sudo apt-get update -qq && sudo apt-get install -y -qq bubblewrap) >/dev/null 2>&1 \
      && echo "made: installed bubblewrap (required by internal/agent's reviewer containment on Linux)" \
      || echo "made: WARNING - could not install bubblewrap; this repo's own go test ./... needs it on Linux for reviewer containment" >&2
  else
    echo "made: WARNING - bubblewrap not found and no apt-get available; this repo's own go test ./... needs it on Linux for reviewer containment" >&2
  fi
fi

echo
echo "Add $INSTALL_DIR to PATH if it is not already there."
