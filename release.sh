#!/usr/bin/env bash
set -euo pipefail

# ---- config ----
CHANGELOG_FILE="${CHANGELOG_FILE:-CHANGELOG.md}"
DEFAULT_FIRST_TAG="${DEFAULT_FIRST_TAG:-v1.0.0}"
COMMIT_PREFIX="${COMMIT_PREFIX:-chore(release): prepare for }"
TAG_MESSAGE_PREFIX="${TAG_MESSAGE_PREFIX:-}"
RELEASE_BRANCH="${RELEASE_BRANCH:-main}"
REMOTE_NAME="${REMOTE_NAME:-origin}"

# ---- helpers ----
die() { echo "error: $*" >&2; exit 1; }
info() { echo ">> $*"; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }

cliff_cmd() {
  if command -v git-cliff >/dev/null 2>&1; then
    echo "git-cliff"
  else
    if git cliff --version >/dev/null 2>&1; then
      echo "git cliff"
    else
      die "git-cliff is not installed (need git-cliff or a working 'git cliff')"
    fi
  fi
}

normalize_version() {
  local v="$1"
  [[ "$v" == v* ]] && echo "$v" || echo "v${v}"
}

latest_v_tag() {
  git tag -l 'v*' --sort=-v:refname | head -n 1 || true
}

git_is_clean() {
  git diff --quiet && git diff --cached --quiet
}

in_git_repo() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1
}

current_branch() {
  git rev-parse --abbrev-ref HEAD
}

ensure_on_release_branch() {
  local b
  b="$(current_branch)"
  [[ "$b" != "HEAD" ]] || die "detached HEAD; checkout ${RELEASE_BRANCH} first"
  [[ "$b" == "$RELEASE_BRANCH" ]] || die "release is allowed only from '${RELEASE_BRANCH}' (current: '${b}')"
}

ensure_not_behind_remote() {
  # Fetch so comparisons are accurate (including tags).
  info "fetching ${REMOTE_NAME} (tags + ${RELEASE_BRANCH})"
  git fetch --tags "$REMOTE_NAME" "$RELEASE_BRANCH" >/dev/null 2>&1 || true

  # Ensure upstream exists; if not, compare to origin/main directly when possible.
  local upstream="${REMOTE_NAME}/${RELEASE_BRANCH}"
  if ! git rev-parse -q --verify "refs/remotes/${upstream}" >/dev/null; then
    die "remote ref not found: ${upstream} (do you have the remote/branch?)"
  fi

  # Count ahead/behind
  # left = commits only on HEAD, right = commits only on upstream
  local counts ahead behind
  counts="$(git rev-list --left-right --count HEAD..."$upstream")"
  ahead="$(echo "$counts" | awk '{print $1}')"
  behind="$(echo "$counts" | awk '{print $2}')"

  # If both > 0 => diverged
  if [[ "${ahead:-0}" -gt 0 && "${behind:-0}" -gt 0 ]]; then
    die "local ${RELEASE_BRANCH} has diverged from ${upstream} (ahead=${ahead}, behind=${behind}). Rebase/merge first."
  fi

  # If behind > 0 => refuse
  if [[ "${behind:-0}" -gt 0 ]]; then
    die "local ${RELEASE_BRANCH} is behind ${upstream} by ${behind} commit(s). Pull/rebase first."
  fi

  # ahead > 0 is OK (you may have local commits), script will create another commit anyway.
  if [[ "${ahead:-0}" -gt 0 ]]; then
    info "note: local ${RELEASE_BRANCH} is ahead of ${upstream} by ${ahead} commit(s) (ok)"
  fi
}

main() {
  need_cmd git
  in_git_repo || die "not a git repository"

  local CLI
  CLI="$(cliff_cmd)"

  ensure_on_release_branch
  git_is_clean || die "working tree is not clean (commit/stash your changes first)"
  ensure_not_behind_remote

  # Ensure config exists (optional but handy)
  if [[ ! -f "cliff.toml" ]]; then
    info "cliff.toml not found -> generating default (git-cliff --init)"
    $CLI --init >/dev/null
    info "created cliff.toml"
  fi

  local requested_version="${1:-}"
  local version=""

  if [[ -n "$requested_version" ]]; then
    version="$(normalize_version "$requested_version")"
  else
    local last_tag
    last_tag="$(latest_v_tag)"

    if [[ -z "$last_tag" ]]; then
      version="$DEFAULT_FIRST_TAG"
      info "no existing v* tags found -> first release defaults to ${version}"
    else
      info "latest tag: ${last_tag}"
      local bumped
      bumped="$($CLI --bumped-version --unreleased || true)"
      bumped="$(echo "$bumped" | tr -d '\r' | xargs || true)"
      [[ -n "$bumped" ]] || die "no releasable changes since ${last_tag} (git-cliff returned empty bumped version)"
      version="$(normalize_version "$bumped")"
    fi
  fi

  if git rev-parse -q --verify "refs/tags/${version}" >/dev/null; then
    die "tag already exists: ${version}"
  fi

  info "release version: ${version}"

  # Generate changelog (NO 'Unreleased': render unreleased commits as this version)
  if [[ -f "$CHANGELOG_FILE" ]]; then
    info "updating ${CHANGELOG_FILE} (prepend new release section)"
    $CLI --unreleased --tag "$version" --prepend "$CHANGELOG_FILE" >/dev/null
  else
    info "creating ${CHANGELOG_FILE}"
    $CLI --unreleased --tag "$version" -o "$CHANGELOG_FILE" >/dev/null
  fi

  info "creating release commit"
  git add "$CHANGELOG_FILE" cliff.toml 2>/dev/null || true
  if git diff --cached --quiet; then
    die "nothing staged after changelog generation (did git-cliff change anything?)"
  fi
  git commit -m "${COMMIT_PREFIX}${version}"

  info "creating annotated tag ${version}"
  git tag -a "$version" -m "${TAG_MESSAGE_PREFIX}${version}"

  info "done."
  echo
  echo "Next:"
  echo "  git push ${REMOTE_NAME} HEAD"
  echo "  git push ${REMOTE_NAME} --tags"
  echo "  # or: git push --follow-tags ${REMOTE_NAME} HEAD"
}

main "$@"
