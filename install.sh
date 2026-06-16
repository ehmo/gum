#!/usr/bin/env bash
# install.sh — single-binary installer for github.com/ehmo/gum (gum-q4b).
#
# Downloads the release archive for the current platform, verifies its
# SHA-256 checksum against the published checksums.txt, optionally verifies
# the SLSA L1 provenance attestation (when slsa-verifier is on PATH), and
# extracts the gum binary into INSTALL_PREFIX (default ~/.local/bin).
#
# Quick start:
#   curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | bash
#
# Pinned version:
#   curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | \
#     GUM_VERSION=v1.0.0 bash
#
# Custom prefix:
#   curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | \
#     GUM_PREFIX=/usr/local/bin bash
#
# Environment variables:
#   GUM_VERSION         Tag to install (default: latest release).
#   GUM_PREFIX          Install directory (default: $HOME/.local/bin).
#   GUM_REPO            GitHub repo (default: ehmo/gum). Override for forks.
#   GUM_SKIP_CHECKSUM   Set to 1 to skip checksum verification (NOT advised).
#   GUM_SKIP_PROVENANCE Set to 1 to skip SLSA provenance verification when
#                       slsa-verifier is installed (default: 0).
#   GUM_DRY_RUN         Set to 1 to print actions without downloading or
#                       installing anything (used by install_test.sh).
#
# Exit codes:
#   0 success
#   1 generic failure (download, extract, install)
#   2 unsupported platform
#   3 checksum verification failed
#   4 provenance verification failed
#   5 PATH conflict refusal (set GUM_FORCE=1 to override)

set -euo pipefail

GUM_REPO="${GUM_REPO:-ehmo/gum}"
GUM_PREFIX="${GUM_PREFIX:-${HOME}/.local/bin}"
GUM_VERSION="${GUM_VERSION:-}"
GUM_SKIP_CHECKSUM="${GUM_SKIP_CHECKSUM:-0}"
GUM_SKIP_PROVENANCE="${GUM_SKIP_PROVENANCE:-0}"
GUM_DRY_RUN="${GUM_DRY_RUN:-0}"
GUM_FORCE="${GUM_FORCE:-0}"
GUM_INSTALL_TMP=""

log()  { printf '[install.sh] %s\n' "$*" >&2; }
warn() { printf '[install.sh] WARN: %s\n' "$*" >&2; }
die()  { printf '[install.sh] ERROR: %s\n' "$*" >&2; exit "${2:-1}"; }

# Platform detection. Returns "<os>_<arch>" matching goreleaser archive names.
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Linux)  os="linux" ;;
    Darwin) os="darwin" ;;
    *) die "unsupported OS: $(uname -s); install.sh covers linux + darwin." 2 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "unsupported arch: $(uname -m); install.sh covers amd64 + arm64." 2 ;;
  esac
  printf '%s_%s\n' "$os" "$arch"
}

# Locate the GitHub release tag to install (latest by default).
resolve_version() {
  if [[ -n "${GUM_VERSION}" ]]; then
    printf '%s\n' "${GUM_VERSION}"
    return
  fi
  local api="https://api.github.com/repos/${GUM_REPO}/releases/latest"
  # Prefer gh if available (handles auth + rate limits); fall back to curl.
  if command -v gh >/dev/null 2>&1; then
    gh api "repos/${GUM_REPO}/releases/latest" --jq '.tag_name'
  else
    curl -fsSL "${api}" | sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' | head -n1
  fi
}

# Pre-install PATH check. Warns when another binary named `gum` is already
# on PATH (most commonly charmbracelet/gum, a TUI-builder with the same
# name). Refuses install when the conflict points outside GUM_PREFIX unless
# GUM_FORCE=1 — a silent overwrite would surprise users.
preinstall_path_check() {
  if ! command -v gum >/dev/null 2>&1; then
    return 0
  fi
  local existing
  existing=$(command -v gum)
  # If the existing binary lives inside GUM_PREFIX, an upgrade is fine.
  if [[ "${existing}" == "${GUM_PREFIX}/"* ]]; then
    log "existing gum at ${existing} will be replaced (same prefix)"
    return 0
  fi
  # Disambiguate this gum from charmbracelet/gum. This gum's --help opens
  # with "gum is a single Go binary that exposes the same dispatch kernel".
  # charmbracelet/gum's --help opens with "A tool for glamorous shell scripts".
  local first_line
  first_line=$("${existing}" --help 2>&1 | head -n1 || true)
  if [[ "${first_line}" == *"glamorous shell scripts"* ]]; then
    warn "PATH conflict: charmbracelet/gum is installed at ${existing}"
    warn "  charmbracelet/gum is a different tool (TUI builder for shell scripts)."
    warn "  This installer ships ehmo/gum (Google API dispatch kernel)."
    warn "  Two options:"
    warn "    1. Install under a different name: GUM_PREFIX=${GUM_PREFIX}; mv ${GUM_PREFIX}/gum ${GUM_PREFIX}/ehmo-gum"
    warn "    2. Uninstall charmbracelet/gum (brew uninstall gum) before re-running this installer."
  elif [[ "${first_line}" == *"dispatch kernel"* ]]; then
    log "existing ehmo/gum at ${existing} will be replaced by version in ${GUM_PREFIX}"
  else
    warn "PATH conflict: a different binary named 'gum' exists at ${existing}"
    warn "  Installer will place the new binary at ${GUM_PREFIX}/gum but PATH"
    warn "  ordering may keep the old one active. Adjust PATH or run with GUM_FORCE=1"
    warn "  to acknowledge."
    if [[ "${GUM_FORCE}" != "1" ]]; then
      die "PATH conflict; set GUM_FORCE=1 to proceed" 5
    fi
  fi
}

verify_checksum() {
  local archive="$1" checksums="$2"
  local want got
  want=$(grep -E "  ${archive}\$" "${checksums}" | awk '{print $1}')
  if [[ -z "${want}" ]]; then
    die "checksum entry for ${archive} not found in checksums.txt" 3
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "${archive}" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "${archive}" | awk '{print $1}')
  else
    die "no sha256sum or shasum on PATH; install one or set GUM_SKIP_CHECKSUM=1" 3
  fi
  if [[ "${want}" != "${got}" ]]; then
    die "checksum mismatch for ${archive}: want ${want}, got ${got}" 3
  fi
  log "checksum verified: ${archive}"
}

verify_provenance() {
  local archive="$1" tag="$2" provenance="$3"
  if ! command -v slsa-verifier >/dev/null 2>&1; then
    log "slsa-verifier not installed; skipping SLSA provenance check"
    log "  install: go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@latest"
    return 0
  fi
  if [[ ! -f "${provenance}" ]]; then
    warn "provenance attestation not downloaded; skipping verification"
    return 0
  fi
  if ! slsa-verifier verify-artifact "${archive}" \
       --provenance-path "${provenance}" \
       --source-uri "github.com/${GUM_REPO}" \
       --source-tag "${tag}"; then
    die "SLSA provenance verification failed for ${archive}" 4
  fi
  log "SLSA provenance verified: ${archive}"
}

postinstall_path_check() {
  local resolved
  resolved=$(command -v gum 2>/dev/null || true)
  if [[ -z "${resolved}" ]]; then
    warn "${GUM_PREFIX} is not on PATH; add it to your shell profile:"
    warn "  export PATH=\"${GUM_PREFIX}:\$PATH\""
    return
  fi
  if [[ "${resolved}" != "${GUM_PREFIX}/gum" ]]; then
    warn "active gum on PATH (${resolved}) is NOT the one just installed (${GUM_PREFIX}/gum)"
    warn "  either reorder PATH or invoke ${GUM_PREFIX}/gum directly."
    return
  fi
  local ver
  ver=$("${resolved}" --version 2>/dev/null | head -n1 || true)
  log "installed: ${resolved} (${ver})"
}

main() {
  local platform tag archive_name archive checksums provenance
  platform=$(detect_platform)
  log "detected platform: ${platform}"

  tag=$(resolve_version)
  if [[ -z "${tag}" ]]; then
    die "could not resolve target version (set GUM_VERSION=vX.Y.Z to pin)"
  fi
  log "target version: ${tag}"

  # goreleaser archive name template:
  #   gum_{version-stripped-of-leading-v}_{os}_{arch}.tar.gz
  local version_no_v ext
  version_no_v="${tag#v}"
  ext="tar.gz"
  archive_name="gum_${version_no_v}_${platform}.${ext}"

  preinstall_path_check

  if [[ "${GUM_DRY_RUN}" == "1" ]]; then
    log "DRY RUN: would download https://github.com/${GUM_REPO}/releases/download/${tag}/${archive_name}"
    log "DRY RUN: would verify checksums.txt entry for ${archive_name}"
    log "DRY RUN: would install gum into ${GUM_PREFIX}"
    return 0
  fi

  GUM_INSTALL_TMP=$(mktemp -d)
  trap 'if [[ -n "${GUM_INSTALL_TMP:-}" ]]; then rm -rf "${GUM_INSTALL_TMP}"; fi' EXIT
  cd "${GUM_INSTALL_TMP}"

  local base="https://github.com/${GUM_REPO}/releases/download/${tag}"
  log "downloading ${base}/${archive_name}"
  curl -fsSL -o "${archive_name}"      "${base}/${archive_name}"
  if [[ "${GUM_SKIP_CHECKSUM}" != "1" ]]; then
    log "downloading checksums.txt"
    curl -fsSL -o "checksums.txt"      "${base}/checksums.txt"
    verify_checksum "${archive_name}" "checksums.txt"
  else
    warn "GUM_SKIP_CHECKSUM=1; skipping checksum verification"
  fi

  if [[ "${GUM_SKIP_PROVENANCE}" != "1" ]]; then
    provenance="gum-${tag}.intoto.jsonl"
    # The attestation may not exist on pre-v0.1.0 tags; tolerate 404.
    if curl -fsSL -o "${provenance}" "${base}/${provenance}" 2>/dev/null; then
      verify_provenance "${archive_name}" "${tag}" "${provenance}"
    else
      log "no provenance attestation at ${base}/${provenance}; skipping"
    fi
  fi

  log "extracting ${archive_name}"
  tar -xzf "${archive_name}"
  if [[ ! -x "./gum" ]]; then
    die "archive did not contain executable gum binary at ./gum"
  fi

  mkdir -p "${GUM_PREFIX}"
  install -m 0755 "./gum" "${GUM_PREFIX}/gum"
  log "installed ${GUM_PREFIX}/gum"

  postinstall_path_check
}

main "$@"
