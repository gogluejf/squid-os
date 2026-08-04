# EPIC: Distribution, Installation, Completion, and Release Documentation
Why: Squid-OS currently has a source-only installer, a system-first Makefile install target, generated-but-uninstalled shell completion, outdated README instructions, and no automated multi-platform release pipeline. Users need one predictable installation experience whether they download a release, build from source, or run under WSL2.
Outcomes: User-local make install and uninstall workflows, a secure release-binary install.sh, friendly Bash and Zsh completion installation, explicit WSL2 support, automated Linux and macOS release artifacts, checksums and provenance, CI validation, and an accurate getting-started README.

## MILESTONE: 1 - User-Local Installation Contract
Pattern: Convention over Configuration, Shared Installation Layout
Objective: Define one XDG-aware installation layout and reusable install behavior shared by source builds, release downloads, and shell completion.
Success: All installation paths target ~/.local by default, support PREFIX overrides, avoid sudo by default, treat WSL2 as Linux, and agree on binary and completion destinations.
Diagram: flowchart TD
    Source[Source checkout] --> Contract[Installation contract]
    Release[Release archive] --> Contract
    WSL[WSL2 Linux environment] --> Contract
    Contract --> Binary[User binary directory]
    Contract --> Bash[Bash completion directory]
    Contract --> Zsh[Zsh completion directory]

### TASK: 1.1 - Define installation paths and platform support
Type: feature
What: Add a small installation helper contract for resolving user-local binary and completion destinations from PREFIX, HOME, XDG_DATA_HOME, OS, and shell inputs.
Why: Make, install.sh, and the CLI completion installer must use identical destinations and explicitly support Linux, WSL2, and macOS without duplicating path logic.
Files: + internal/install
Files: + internal/install/paths_test.go
Snippet: package install\n\ntype Platform string\n\nconst (\n    PlatformLinux Platform = "linux"\n    PlatformDarwin Platform = "darwin"\n)\n\ntype Layout struct {\n    Prefix        string\n    BinaryPath    string\n    BashCompletionPath string\n    ZshCompletionPath  string\n}\n\nfunc ResolveLayout(platform Platform, home, prefix, xdgDataHome string) (Layout, error) {\n    // Resolve one user-local installation contract.\n}
Acceptance: Default prefix resolves to HOME/.local.
Acceptance: PREFIX overrides the default for source and system-wide installs.
Acceptance: XDG_DATA_HOME controls user completion data locations where applicable.
Acceptance: Linux and WSL2 resolve through the same Linux layout.
Acceptance: macOS resolves a documented user-local layout.
Acceptance: Unsupported native Windows returns a clear error.
Verification: go test ./internal/install
Verification: go test ./...

### TASK: 1.2 - Add PATH readiness and shell guidance
Type: feature
What: Add reusable checks that determine whether the installed binary directory is currently discoverable and produce shell-specific activation guidance.
Why: Installers should make squid-os immediately understandable without silently or repeatedly editing shell startup files.
Files: ~ internal/install
Files: + internal/install/shell_test.go
Snippet: package install\n\ntype Shell string\n\nconst (\n    ShellBash Shell = "bash"\n    ShellZsh  Shell = "zsh"\n)\n\ntype Activation struct {\n    BinaryOnPath bool\n    RestartCommand string\n    ProfileHint string\n}\n\nfunc CheckActivation(shell Shell, binaryDir, currentPath string) Activation {\n    // Report exact next steps without modifying shell profiles.\n}
Acceptance: PATH checks use path components rather than substring matching.
Acceptance: Bash and Zsh guidance names the correct startup profile and restart command.
Acceptance: WSL2 Bash receives normal Linux guidance.
Acceptance: No helper silently edits .bashrc, .profile, or .zshrc.
Acceptance: Repeated checks remain idempotent and produce stable output.
Verification: go test ./internal/install

### TASK: 1.3 - Add cross-platform installation contract tests
Type: test
What: Add table-driven fixtures covering Linux, WSL2, macOS, XDG overrides, custom PREFIX values, missing HOME, and unsupported platforms.
Why: The installation contract is security- and usability-sensitive and must remain consistent before Make, shell scripts, and CLI commands depend on it.
Files: + internal/install/install_test.go
Snippet: package install\n\nfunc TestInstallationMatrix(t *testing.T) {\n    // Linux user-local layout\n    // WSL2 Linux-equivalent layout\n    // macOS user-local layout\n    // XDG and PREFIX overrides\n    // invalid environment failures\n}
Acceptance: Tests cover Linux amd64 and arm64 layouts.
Acceptance: Tests identify WSL2 but preserve Linux artifact and path behavior.
Acceptance: Tests cover macOS amd64 and arm64 layouts.
Acceptance: Tests cover paths containing spaces.
Acceptance: Tests cover missing or malformed environment values with clear failures.
Verification: go test ./internal/install -run Installation
Verification: go test ./...

## MILESTONE: 2 - Source Install and Shell Completion
Pattern: Idempotent Installer, Command Adapter
Objective: Make a source checkout install a production binary and shell completion into the shared user-local layout, with explicit uninstall behavior and no default sudo requirement.
Success: make install builds and installs squid-os under ~/.local by default, completion install and uninstall work for Bash and Zsh, raw Cobra generation remains available, and repeated operations are safe.
Diagram: flowchart LR
    Checkout[Git checkout] --> Make[make install]
    Make --> Build[Versioned binary]
    Make --> Complete[Completion installer]
    Build --> Prefix[Selected prefix]
    Complete --> Bash[Bash completion]
    Complete --> Zsh[Zsh completion]
    Prefix --> Verify[Installation verification]

### TASK: 2.1 - Implement user-local Make install lifecycle
Type: chore
What: Replace the fallback-copy Makefile with explicit build, install, uninstall, completion, clean, test, and verify targets using PREFIX and DESTDIR.
Why: Source users need deterministic installation without hidden sudo attempts or fallback destinations.
Files: ~ Makefile
Snippet: BINARY := squid-os\nPREFIX ?= $(HOME)/.local\nDESTDIR ?=\nBINDIR := $(DESTDIR)$(PREFIX)/bin\n\n.PHONY: build install uninstall completion test verify clean\n\ninstall: build\n\t# Install binary and detected-shell completion into the shared layout.\n\nuninstall:\n\t# Remove only files owned by squid-os installation.\n
Acceptance: make build embeds the canonical version and git commit metadata.
Acceptance: make install defaults to HOME/.local/bin/squid-os.
Acceptance: make install PREFIX=/usr/local supports an explicit system-wide destination.
Acceptance: DESTDIR supports packaging without changing logical PREFIX.
Acceptance: make install creates missing destination directories.
Acceptance: make uninstall removes the binary and installed completion without deleting unrelated directories.
Acceptance: No Make target silently invokes sudo.
Verification: make clean build
Verification: make install PREFIX=/tmp/tmp.AV3V6KgFRf
Verification: make test
Verification: make verify

### TASK: 2.2 - Add completion install and uninstall commands
Type: feature
What: Extend the Cobra completion command with install and uninstall subcommands for Bash and Zsh, using the shared installation layout and optional shell auto-detection.
Why: Users should not need to redirect generated scripts manually, while package managers must retain raw completion generation.
Files: ~ internal/cli
Files: ~ internal/install
Files: + internal/cli/completion_install_test.go
Snippet: type CompletionInstallOptions struct {\n    Shell  install.Shell\n    Prefix string\n}\n\nfunc InstallCompletion(options CompletionInstallOptions) (string, error) {\n    // Generate completion and atomically install it at the resolved path.\n}\n\nfunc UninstallCompletion(options CompletionInstallOptions) error {\n    // Remove only the selected squid-os completion file.\n}
Acceptance: squid-os completion install auto-detects Bash or Zsh from SHELL.
Acceptance: squid-os completion install bash and zsh work explicitly.
Acceptance: squid-os completion uninstall removes the matching completion file.
Acceptance: Raw squid-os completion bash and zsh continue writing scripts to stdout.
Acceptance: Installation uses atomic file replacement and creates parent directories.
Acceptance: The command prints the installed path and any required shell restart guidance.
Acceptance: The completion command never modifies PATH or shell startup files.
Verification: go test ./internal/cli -run Completion
Verification: go test ./...

### TASK: 2.3 - Integrate and test source installation end to end
Type: test
What: Wire Make installation to generated completion artifacts and add an isolated-prefix smoke test for install, repeat install, command execution, completion presence, and uninstall.
Why: The source workflow must prove that binary and completion installation operate together without touching the developer machine during CI.
Files: ~ Makefile
Files: + scripts/test-install-source.sh
Snippet: #!/usr/bin/env bash\nset -euo pipefail\n\n# Install into a temporary PREFIX.\n# Verify binary version and completion files.\n# Repeat installation to prove idempotence.\n# Uninstall and verify owned files are removed.\n
Acceptance: The smoke test installs entirely under a temporary directory.
Acceptance: The installed binary runs --version and --help successfully.
Acceptance: Bash and Zsh completion files are generated from the installed binary.
Acceptance: A second install succeeds without duplicate files or profile changes.
Acceptance: Uninstall leaves unrelated sentinel files untouched.
Acceptance: The test runs on Linux and WSL2 without sudo.
Verification: bash scripts/test-install-source.sh
Verification: make verify

## MILESTONE: 3 - Release Binary Installer
Pattern: Bootstrap Installer, Fail Closed Verification
Objective: Replace the source-building install.sh with a stable downloader that selects a published artifact, verifies it, installs user-locally, and installs completion.
Success: curl install.sh downloads the correct Linux, WSL2, or macOS artifact, verifies authenticated release metadata when available and SHA-256 integrity always, installs without sudo by default, and provides exact PATH activation guidance.
Diagram: sequenceDiagram
    participant User
    participant Script as install.sh
    participant Release as GitHub Release
    participant Local as User local prefix
    User->>Script: Run installer
    Script->>Script: Detect OS architecture shell WSL2
    Script->>Release: Download archive metadata checksum signature
    Script->>Script: Verify provenance and checksum
    Script->>Local: Atomically install binary
    Script->>Local: Install shell completion
    Script-->>User: Print version paths and activation steps

### TASK: 3.1 - Resolve release version and platform artifact
Type: feature
What: Rewrite install.sh detection and release resolution for latest or pinned versions across Linux, WSL2, and macOS on amd64 and arm64.
Why: The downloader must select one deterministic published artifact and fail clearly on unsupported systems before making changes.
Files: ~ install.sh
Files: + scripts/test-install-release.sh
Snippet: #!/bin/sh\nset -eu\n\n# Inputs: SQUID_OS_VERSION, SQUID_OS_PREFIX, SQUID_OS_GITHUB_REPOSITORY.\n# Detect linux or darwin and amd64 or arm64.\n# Treat WSL2 as linux and select the linux artifact.\n# Resolve latest release only when no version is pinned.\n
Acceptance: Linux amd64 and arm64 map to matching release archives.
Acceptance: WSL2 amd64 and arm64 map to Linux release archives.
Acceptance: macOS amd64 and arm64 map to Darwin release archives.
Acceptance: SQUID_OS_VERSION pins a specific tag without querying latest.
Acceptance: Repository and download base can be overridden for deterministic tests and mirrors.
Acceptance: Native Windows and unsupported architectures fail before downloading.
Acceptance: The script is POSIX shell compatible or clearly requires Bash with a validated interpreter.
Verification: bash scripts/test-install-release.sh detection
Verification: shellcheck install.sh scripts/test-install-release.sh

### TASK: 3.2 - Verify release integrity and authenticity
Type: feature
What: Download release checksums and verification metadata, authenticate them when supported, and verify the selected archive before extraction.
Why: Checksums catch corruption, while signatures or CI provenance provide a stronger trust signal against artifact substitution.
Files: ~ install.sh
Files: + docs/release-security.md
Files: ~ scripts/test-install-release.sh
Snippet: # Verification policy\n# 1. Verify signed checksum or release attestation when the configured tool is available.\n# 2. Always verify the selected archive against checksums.txt.\n# 3. Fail closed on mismatch, malformed metadata, or missing checksum entry.\n# 4. Document the curl installer bootstrap trust boundary.\n
Acceptance: CI-generated checksums.txt is mandatory for installation.
Acceptance: The selected archive SHA-256 must match exactly before extraction.
Acceptance: The installer supports sha256sum and macOS shasum -a 256.
Acceptance: Checksum mismatch, missing entry, and malformed metadata stop installation without replacing an existing binary.
Acceptance: The plan selects and documents either GitHub artifact attestations, Cosign, or Minisign as the authenticity mechanism.
Acceptance: The public verification identity or key and rotation procedure are documented outside release artifacts.
Acceptance: Documentation explains that curl-pipe-shell still trusts the fetched installer.
Verification: bash scripts/test-install-release.sh verification
Verification: shellcheck install.sh

### TASK: 3.3 - Install release binary and completion safely
Type: feature
What: Install the verified binary atomically under the selected prefix, install completion for the detected shell, and report activation or PATH guidance.
Why: A successful download must produce the same predictable layout as make install without leaving partial upgrades or surprising profile edits.
Files: ~ install.sh
Files: ~ scripts/test-install-release.sh
Snippet: #!/bin/sh\n\n# Extract into a temporary directory.\n# Validate the binary with --version.\n# Atomically replace PREFIX/bin/squid-os.\n# Generate and install completion using the installed binary.\n# Print exact activation guidance without silently editing profiles.\n
Acceptance: Default installation uses HOME/.local and requires no sudo.
Acceptance: SQUID_OS_PREFIX supports an explicit alternative destination.
Acceptance: Existing installations remain intact if download, verification, extraction, or validation fails.
Acceptance: Bash or Zsh completion is installed when the shell is recognized.
Acceptance: Unknown shells still install the binary and print manual completion instructions.
Acceptance: The final output reports installed version, binary path, completion path, and whether PATH is ready.
Acceptance: Repeated installation and upgrades are idempotent.
Acceptance: Temporary files are cleaned on success, failure, and interruption.
Verification: bash scripts/test-install-release.sh install
Verification: shellcheck install.sh

### TASK: 3.4 - Test release installer against local fixtures
Type: test
What: Build local release fixtures and exercise install.sh through a local HTTP server for supported platforms, pinned/latest versions, upgrades, failures, and rollback.
Why: Installer regressions must be reproducible in CI without mutating user directories or relying on GitHub availability.
Files: ~ scripts/test-install-release.sh
Files: + testdata/install
Snippet: #!/usr/bin/env bash\nset -euo pipefail\n\n# Build fake archives and checksum metadata.\n# Serve fixtures from localhost.\n# Run install.sh with isolated HOME and PREFIX.\n# Assert success, rejection, rollback, cleanup, and completion behavior.\n
Acceptance: Tests cover Linux amd64 and arm64 artifact names.
Acceptance: Tests cover WSL2 selecting Linux artifacts through injectable detection fixtures.
Acceptance: Tests cover Darwin amd64 and arm64 artifact names without requiring macOS execution.
Acceptance: Tests cover pinned and mocked-latest releases.
Acceptance: Tests cover checksum mismatch, missing metadata, HTTP failure, invalid archive, and invalid binary rollback.
Acceptance: Tests prove the installer never writes outside isolated HOME, PREFIX, and temporary directories.
Verification: bash scripts/test-install-release.sh all

## MILESTONE: 4 - Continuous Integration and Release Automation
Pattern: Reproducible Build, Tagged Release Pipeline, Supply Chain Attestation
Objective: Validate every change and publish reproducible Linux and macOS release archives, checksums, provenance, and release notes from version tags.
Success: Pull requests run code and installer gates; version tags produce amd64 and arm64 artifacts for Linux and macOS; checksums and attestations are published automatically; no release script is rewritten per version.
Diagram: flowchart TD
    Change[Push or pull request] --> CI[Validation workflow]
    CI --> Go[Build test vet]
    CI --> Shell[Shell lint and installer fixtures]
    Tag[Version tag] --> Release[Release workflow]
    Release --> Matrix[Linux Darwin amd64 arm64]
    Matrix --> Archives[Versioned archives]
    Archives --> Checksums[Checksums]
    Archives --> Provenance[Attestations]
    Checksums --> GitHub[GitHub Release]
    Provenance --> GitHub

### TASK: 4.1 - Add continuous integration quality gates
Type: chore
What: Add a GitHub Actions workflow for Go build/test/vet, formatting checks, shell lint, source-install smoke tests, and release-installer fixture tests.
Why: Installation and release code needs the same mandatory validation as application code before merge.
Files: + .github/workflows/ci.yml
Files: ~ Makefile
Snippet: name: CI\n\non:\n  push:\n  pull_request:\n\njobs:\n  go:\n    # Build, test, vet, and formatting checks.\n  install:\n    # ShellCheck and isolated installer tests.\n
Acceptance: CI runs on pushes and pull requests.
Acceptance: CI runs go build, go test, go vet, and gofmt verification.
Acceptance: CI runs ShellCheck on maintained shell scripts.
Acceptance: CI runs source and release installer tests in isolated directories.
Acceptance: CI uses pinned major action versions and least-privilege permissions.
Acceptance: make verify reproduces the primary CI checks locally.
Verification: make verify
Verification: actionlint .github/workflows/ci.yml

### TASK: 4.2 - Configure reproducible cross-platform releases
Type: chore
What: Add GoReleaser configuration for static Linux and macOS amd64 and arm64 binaries, deterministic archives, checksums, changelog generation, and version metadata.
Why: Release artifacts must match install.sh naming exactly and be generated consistently from tags rather than developer machines.
Files: + .goreleaser.yaml
Files: ~ internal/version/version.go
Files: ~ Makefile
Snippet: version: 2\n\nbuilds:\n  - id: squid-os\n    binary: squid-os\n    env:\n      - CGO_ENABLED=0\n    goos:\n      - linux\n      - darwin\n    goarch:\n      - amd64\n      - arm64\n\nchecksum:\n  name_template: checksums.txt\n
Acceptance: Artifacts are produced for linux amd64, linux arm64, darwin amd64, and darwin arm64.
Acceptance: Archive names exactly match install.sh resolution rules.
Acceptance: Builds use CGO_ENABLED=0 unless a verified platform dependency requires otherwise.
Acceptance: Version and git commit metadata derive from the release tag and commit.
Acceptance: checksums.txt includes every published archive.
Acceptance: Snapshot releases can be generated locally without publishing.
Verification: goreleaser check
Verification: goreleaser release --snapshot --clean
Verification: make verify

### TASK: 4.3 - Publish tagged releases with provenance
Type: chore
What: Add a tag-triggered GitHub Actions workflow that validates, runs GoReleaser, publishes artifacts and checksums, and emits the selected authenticity metadata.
Why: Users need automatic releases whose artifacts can be verified by install.sh and traced to an authorized CI workflow.
Files: + .github/workflows/release.yml
Files: ~ .goreleaser.yaml
Files: ~ docs/release-security.md
Snippet: name: Release\n\non:\n  push:\n    tags:\n      - "v*"\n\npermissions:\n  contents: write\n  id-token: write\n  attestations: write\n\njobs:\n  release:\n    # Validate tag and tests.\n    # Build and publish through GoReleaser.\n    # Publish GitHub artifact attestations or selected signatures.\n
Acceptance: Only semantic version tags matching the documented policy publish releases.
Acceptance: The workflow runs tests before publishing.
Acceptance: GitHub permissions are scoped to the release job and required capabilities.
Acceptance: Archives, checksums.txt, and authenticity metadata appear on one GitHub Release.
Acceptance: Release notes are generated consistently and can be edited before final publication when configured.
Acceptance: The workflow fails if expected artifacts or verification metadata are missing.
Acceptance: The install script does not need modification for each version.
Verification: actionlint .github/workflows/release.yml
Verification: goreleaser check

### TASK: 4.4 - Validate release artifacts and installer compatibility
Type: test
What: Add a CI dry-run job that inspects snapshot archives, verifies checksums and version metadata, and installs the host-compatible artifact through install.sh fixtures.
Why: GoReleaser and install.sh can each pass independently while disagreeing on names or archive contents; this test closes that integration gap.
Files: ~ .github/workflows/ci.yml
Files: ~ scripts/test-install-release.sh
Snippet: # Release compatibility checks\n# - Build snapshot artifacts.\n# - Assert the complete OS and architecture name matrix.\n# - Verify archive contents and checksums.\n# - Serve artifacts locally and run install.sh.\n# - Execute the installed binary.\n
Acceptance: The expected four release archives are present.
Acceptance: Each archive contains a squid-os executable and required release metadata only.
Acceptance: Every archive has a checksums.txt entry.
Acceptance: The installed host artifact reports the snapshot version and commit.
Acceptance: Any artifact naming drift between GoReleaser and install.sh fails CI.
Verification: goreleaser release --snapshot --clean
Verification: bash scripts/test-install-release.sh dist

## MILESTONE: 5 - Getting Started and Maintainer Documentation
Pattern: Documentation as Product, Single Source of Truth
Objective: Replace outdated startup documentation with a concise first-run path, source and release installation guidance, WSL2 support, completion behavior, upgrades, uninstall, and maintainer release procedures.
Success: A new user can install and run Squid-OS from README alone; WSL2 status and native Windows scope are explicit; maintainers can cut and verify a release; website download guidance matches the repository.
Diagram: flowchart TD
    Visitor[New visitor] --> README[README quick start]
    README --> Curl[Curl release install]
    README --> Source[Source make install]
    README --> WSL[WSL2 guidance]
    Curl --> FirstRun[First run]
    Source --> FirstRun
    WSL --> FirstRun
    Maintainer[Maintainer] --> ReleaseDoc[Release runbook]
    ReleaseDoc --> Tag[Tag and publish]
    Tag --> Verify[Verify release and installer]

### TASK: 5.1 - Rewrite README quick start and installation guide
Type: doc
What: Rewrite the README opening, prerequisites, installation, first run, CLI examples, completion, upgrade, uninstall, configuration, and platform support sections.
Why: The current README documents removed headless/incognito flags, obsolete files, a source-building installer, and stale Go requirements.
Files: ~ README.md
Snippet: # Squid-OS\n\n<!-- Product summary and current screenshot -->\n\n## Install\n\n### Release binary\n```sh\ncurl -fsSL https://raw.githubusercontent.com/gogluejf/squid-os/master/install.sh | sh\n```\n\n### Build from source\n```sh\ngit clone https://github.com/gogluejf/squid-os.git\ncd squid-os\nmake install\n```\n\n## First run\n```sh\nsquid-os\nsquid-os run --prompt "hello"\n```\n
Acceptance: README leads with a concise product description and valid screenshot path.
Acceptance: Release installation and source installation are distinct and accurate.
Acceptance: README documents HOME/.local default, PATH guidance, PREFIX override, upgrade, and uninstall.
Acceptance: README documents automatic and manual Bash/Zsh completion installation.
Acceptance: README explicitly supports Linux, WSL2, and macOS and states native Windows is unsupported.
Acceptance: CLI examples use current tui and run grammar with no removed flags.
Acceptance: Referenced source files and commands exist.
Acceptance: Security wording explains checksum/provenance verification and curl installer trust.
Verification: make docs-check
Verification: make verify

### TASK: 5.2 - Document platform support and installation troubleshooting
Type: doc
What: Add a focused installation document covering Linux, WSL2, macOS, shell discovery, PATH activation, completion locations, custom prefixes, proxies, and common failures.
Why: The README should stay concise while users still need precise recovery steps for environment-specific installation issues.
Files: + docs/installation.md
Files: ~ README.md
Snippet: # Installation\n\n## Supported platforms\n- Linux amd64 and arm64\n- WSL2 amd64 and arm64 using Linux artifacts\n- macOS amd64 and arm64\n- Native Windows not currently supported\n\n## Troubleshooting\n<!-- PATH, shell completion, permissions, proxies, checksum failures, custom prefixes -->\n
Acceptance: WSL2 instructions explain that it uses Linux artifacts and installs inside the Linux filesystem.
Acceptance: Documentation recommends cloning/building inside the WSL filesystem rather than mounted Windows paths for performance.
Acceptance: Bash and Zsh completion paths and reload commands are documented.
Acceptance: Custom PREFIX, XDG_DATA_HOME, and noninteractive environment variables are documented.
Acceptance: Troubleshooting covers command not found, completion not loading, permission errors, verification failures, and unsupported platforms.
Acceptance: No documentation recommends sudo for the default user-local install.
Verification: make docs-check

### TASK: 5.3 - Add maintainer release runbook
Type: doc
What: Document versioning, pre-release checks, tag creation, automated publication, artifact verification, installer smoke tests, rollback, and verification-identity maintenance.
Why: Release automation is only reliable when maintainers have an explicit operational and security procedure.
Files: + docs/releasing.md
Files: ~ docs/release-security.md
Snippet: # Releasing Squid-OS\n\n## Before tagging\n```sh\nmake verify\nmake release-snapshot\n```\n\n## Publish\n```sh\ngit tag vX.Y.Z\ngit push origin vX.Y.Z\n```\n\n## Verify\n<!-- GitHub Release artifacts, checksums, attestations, and install.sh smoke test -->\n
Acceptance: The runbook defines the semantic version and tag policy.
Acceptance: The runbook requires a clean tree and passing local verification before tagging.
Acceptance: The runbook explains the automated workflow and expected artifact matrix.
Acceptance: The runbook includes post-release install.sh verification for latest and pinned versions.
Acceptance: Rollback or yanking guidance avoids silently replacing immutable release artifacts.
Acceptance: Authenticity identity/key rotation and compromise response are documented.
Verification: make docs-check

### TASK: 5.4 - Align website downloads and validate documentation
Type: doc
What: Update website installation/download guidance to match README and add automated checks for stale commands, local links, referenced files, and release URLs.
Why: Multiple public installation surfaces must not diverge as the CLI and release workflow evolve.
Files: ~ website/download.html
Files: ~ website/index.html
Files: + scripts/check-docs.sh
Files: ~ Makefile
Snippet: #!/usr/bin/env bash\nset -euo pipefail\n\n# Validate local Markdown links and referenced paths.\n# Reject removed CLI flags and obsolete installer commands.\n# Check that documented release artifact names match configuration.\n
Acceptance: Website download instructions show release install and source make install separately.
Acceptance: Website states Linux, WSL2, macOS, architecture support, and native Windows scope accurately.
Acceptance: Website completion and PATH wording matches README.
Acceptance: Documentation checks reject removed --headless and CLI --incognito examples.
Acceptance: Documentation checks validate local README/docs links and referenced files.
Acceptance: make docs-check runs locally and in CI.
Verification: bash scripts/check-docs.sh
Verification: make docs-check
Verification: make verify
