#!/usr/bin/env bash
# install_test.sh — smoke tests for install.sh (gum-q4b).
#
# Run from repo root:
#   ./install_test.sh
#
# Exits 0 on success, non-zero on the first failure. Designed to run in CI
# without network access (all network paths are short-circuited by
# GUM_DRY_RUN=1 or by setting GUM_VERSION to avoid the latest-release API).

set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${HERE}/install.sh"

pass() { printf '[install_test.sh] PASS: %s\n' "$*"; }
fail() { printf '[install_test.sh] FAIL: %s\n' "$*" >&2; exit 1; }

[[ -x "${SCRIPT}" ]] || fail "install.sh is not executable"
pass "install.sh exists and is executable"

if command -v shellcheck >/dev/null 2>&1; then
  shellcheck "${SCRIPT}" || fail "shellcheck reported issues"
  pass "shellcheck clean"
else
  printf '[install_test.sh] SKIP: shellcheck not installed\n'
fi

# Dry-run path: must succeed without network and report the three DRY RUN lines.
out=$(GUM_DRY_RUN=1 GUM_VERSION=v1.0.0 "${SCRIPT}" 2>&1)
echo "${out}" | grep -q "DRY RUN: would download" || fail "dry-run missing download line"
echo "${out}" | grep -q "DRY RUN: would verify checksums.txt" || fail "dry-run missing checksum line"
echo "${out}" | grep -q "DRY RUN: would install gum into" || fail "dry-run missing install line"
pass "dry-run prints all three planned actions"

# Custom prefix flows through to the install line.
out=$(GUM_DRY_RUN=1 GUM_VERSION=v1.0.0 GUM_PREFIX=/tmp/gum-test-prefix "${SCRIPT}" 2>&1)
echo "${out}" | grep -q "/tmp/gum-test-prefix" || fail "GUM_PREFIX did not propagate"
pass "GUM_PREFIX override propagates"

# Unsupported-platform detection: simulate via a wrapper that overrides uname.
tmp=$(mktemp -d)
trap 'rm -rf "${tmp}"' EXIT
cat > "${tmp}/uname" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == "-s" ]]; then echo "Plan9"; elif [[ "$1" == "-m" ]]; then echo "riscv64"; else /usr/bin/uname "$@"; fi
EOF
chmod +x "${tmp}/uname"
set +e
PATH="${tmp}:${PATH}" GUM_DRY_RUN=1 GUM_VERSION=v1.0.0 "${SCRIPT}" >"${tmp}/out" 2>&1
rc=$?
set -e
[[ "${rc}" -eq 2 ]] || fail "unsupported OS should exit 2, got ${rc}"
grep -q "unsupported OS" "${tmp}/out" || fail "unsupported-OS error message missing"
pass "unsupported OS exits 2 with diagnostic"

# Real install path with mocked downloads: catches EXIT-trap regressions under
# set -u without touching the network.
real_tmp=$(mktemp -d)
trap 'rm -rf "${tmp}" "${real_tmp}" /tmp/gum-install-real.out /tmp/gum-install-real-version.out' EXIT
mkdir -p "${real_tmp}/bin" "${real_tmp}/prefix" "${real_tmp}/payload"
cat > "${real_tmp}/payload/gum" <<'EOF'
#!/usr/bin/env bash
echo "1.0.0"
EOF
chmod +x "${real_tmp}/payload/gum"
(cd "${real_tmp}/payload" && tar -czf "${real_tmp}/gum_1.0.0_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" gum)
archive="${real_tmp}/gum_1.0.0_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz"
if command -v sha256sum >/dev/null 2>&1; then
  checksum=$(sha256sum "${archive}" | awk '{print $1}')
else
  checksum=$(shasum -a 256 "${archive}" | awk '{print $1}')
fi
printf '%s  %s\n' "${checksum}" "$(basename "${archive}")" > "${real_tmp}/checksums.txt"
cat > "${real_tmp}/bin/curl" <<EOF
#!/usr/bin/env bash
set -euo pipefail
out=""
while [[ \$# -gt 0 ]]; do
  case "\$1" in
    -o) out="\$2"; shift 2 ;;
    -*) shift ;;
    *) url="\$1"; shift ;;
  esac
done
case "\${url:-}" in
  *"$(basename "${archive}")") cp "${archive}" "\${out}" ;;
  *"checksums.txt") cp "${real_tmp}/checksums.txt" "\${out}" ;;
  *"intoto.jsonl") exit 22 ;;
  *) echo "unexpected curl url: \${url:-}" >&2; exit 1 ;;
esac
EOF
chmod +x "${real_tmp}/bin/curl"
PATH="${real_tmp}/bin:${PATH}" GUM_VERSION=v1.0.0 GUM_PREFIX="${real_tmp}/prefix" "${SCRIPT}" >/tmp/gum-install-real.out 2>&1
"${real_tmp}/prefix/gum" >/tmp/gum-install-real-version.out
grep -q "1.0.0" /tmp/gum-install-real-version.out || fail "mock install did not place executable gum"
pass "mocked real install exits cleanly after cleanup trap"

printf '[install_test.sh] all tests passed\n'
