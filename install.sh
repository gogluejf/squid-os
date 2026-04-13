#!/usr/bin/env bash
# rig-chat/install.sh — bootstrap Go toolchain + build rig-chat
#
# Fully idempotent: every step checks before acting, safe to re-run anytime.
# Each step prints ✓ (already done) or ↓/⚙ (action taken).
#
# What it does (5 steps):
#   1. Go SDK     — downloads Go 1.24.2 to /usr/local/go if missing
#   2. PATH       — appends Go bin dirs to ~/.bashrc if not already there
#   3. Config     — creates ${XDG_CONFIG_HOME:-~/.config}/rig-chat/ dirs + defaults
#   4. Modules    — runs `go mod tidy` in repo root if go.sum is missing
#   5. Build      — compiles rig-chat static binary into ./bin/rig-chat
#
# Flags:
#   --clean    force-rebuild (re-fetch modules + recompile binary)
#
# Usage:
#   ./install.sh
#   ./install.sh --clean

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}"
BIN_DIR="${REPO_ROOT}/bin"

GO_VERSION="1.24.2"
GO_ARCHIVE="go${GO_VERSION}.linux-amd64.tar.gz"
GO_SDK="/usr/local/go"
GO_BIN="${GO_SDK}/bin/go"
BINARY="rig-chat"
BINARY_PATH="${BIN_DIR}/${BINARY}"
LEGACY_BINARY="${REPO_ROOT}/${BINARY}"

CONFIG_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/rig-chat"

GREEN='\033[0;32m'
CYAN='\033[0;36m'
DIM='\033[2m'
BOLD='\033[1m'
RESET='\033[0m'

CLEAN=false
[[ "${1:-}" == "--clean" ]] && CLEAN=true

echo -e "\n${BOLD}  rig-chat install${RESET}"
echo -e "  ${DIM}Go ${GO_VERSION} · Bubble Tea · Cobra · Glamour · Lip Gloss · Chroma${RESET}\n"

if [[ ! -f "${REPO_ROOT}/go.mod" ]]; then
    echo "Missing ${REPO_ROOT}/go.mod; run this script from the repository root." >&2
    exit 1
fi

# ─────────────────────────────────────────────────────────────────────────────
# 1. Go SDK
# ─────────────────────────────────────────────────────────────────────────────

if [[ -x "${GO_BIN}" ]]; then
    current="$("${GO_BIN}" version 2>/dev/null | awk '{print $3}' | sed 's/go//')"
    echo -e "  ${GREEN}✓${RESET}  Go ${current} ${DIM}(${GO_SDK})${RESET}"
else
    echo -e "  ${CYAN}↓${RESET}  Downloading Go ${GO_VERSION}..."
    wget -q "https://go.dev/dl/${GO_ARCHIVE}" -O "/tmp/${GO_ARCHIVE}"
    sudo rm -rf "${GO_SDK}"
    sudo tar -C /usr/local -xzf "/tmp/${GO_ARCHIVE}"
    rm -f "/tmp/${GO_ARCHIVE}"
    echo -e "  ${GREEN}✓${RESET}  Go ${GO_VERSION} installed ${DIM}→ ${GO_SDK}${RESET}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# 2. PATH
# ─────────────────────────────────────────────────────────────────────────────


if ! grep -q '/usr/local/go/bin' "${HOME}/.bashrc" 2>/dev/null; then
    {
        echo 'export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH'
    } >> "${HOME}/.bashrc"
    echo -e "  ${GREEN}✓${RESET}  PATH added to ${DIM}~/.bashrc${RESET}"
    source ~/.bashrc
else
    echo -e "  ${GREEN}✓${RESET}  PATH ${DIM}(already in ~/.bashrc)${RESET}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# 3. Go modules (dependencies)
# ─────────────────────────────────────────────────────────────────────────────

cd "${REPO_ROOT}"

if [[ ! -f go.sum ]] || $CLEAN; then
    echo -e "  ${CYAN}↓${RESET}  Fetching Go modules..."
    "${GO_BIN}" mod tidy 2>&1 | sed 's/^/     /'
    echo -e "  ${GREEN}✓${RESET}  Dependencies resolved"
else
    echo -e "  ${GREEN}✓${RESET}  Dependencies ${DIM}(go.sum exists)${RESET}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# 4. Build
# ─────────────────────────────────────────────────────────────────────────────

if [[ -f "${BINARY_PATH}" ]] && ! $CLEAN; then
    echo -e "  ${GREEN}✓${RESET}  ${BINARY_PATH} ${DIM}(already built — use --clean to rebuild)${RESET}"
else
    mkdir -p "${BIN_DIR}"
    $CLEAN && rm -f "${BINARY_PATH}" "${LEGACY_BINARY}"
    echo -e "  ${CYAN}⚙${RESET}  Compiling ${BINARY_PATH}..."
    CGO_ENABLED=0 "${GO_BIN}" build -ldflags="-s -w" -o "${BINARY_PATH}" .
    echo -e "  ${GREEN}✓${RESET}  ${BINARY_PATH} ${DIM}($(du -h "${BINARY_PATH}" | cut -f1) static binary)${RESET}"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Done
# ─────────────────────────────────────────────────────────────────────────────

echo ""
echo -e "  $("${BINARY_PATH}" --help 2>&1 | head -1)"
echo ""
echo -e "  ${GREEN}${BOLD}Done.${RESET} Run:"
echo -e "    ${CYAN}./bin/${BINARY}${RESET}                                      ${DIM}# TUI${RESET}"
echo -e "    ${CYAN}./bin/${BINARY} --headless --prompt \"hello\"${RESET}        ${DIM}# stdout${RESET}"
echo -e "    ${CYAN}rig chat${RESET}                                          ${DIM}# via rig CLI${RESET}"
echo ""
