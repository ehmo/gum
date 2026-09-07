---
title: Install
description: "Install gum from release archives, the install script, Homebrew, or source."
---

# Install

`gum` publishes release archives for macOS and Linux on AMD64 and ARM64.
Download the archive for your platform from the
[GitHub releases page](https://github.com/ehmo/gum/releases), verify it with
`checksums.txt`, then put the binary on your `PATH`.

```bash
tar -xzf gum_<version>_<os>_<arch>.tar.gz
install -m 0755 gum ~/.local/bin/gum
gum --version
```

## Install script

The install script fetches the latest release archive and installs the binary:

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | bash
```

Pin a version with `GUM_VERSION`:

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.3.0 bash
```

## Homebrew

Use the qualified formula name. The unqualified `brew install gum` points to a
different CLI from Charmbracelet.

```bash
brew tap ehmo/tap https://github.com/ehmo/homebrew-tap
brew install ehmo/tap/gum
gum setup --dry-run
gum doctor
```

Upgrade an existing installation with:

```bash
brew update
brew upgrade ehmo/tap/gum
gum --version
```

If a firewall prompts after an upgrade, approve the required Google endpoints
for the new executable. See [OAuth connection timeouts](support.md#oauth-connection-timeouts).

## Build from source

The Go module lives under `apps/gum`:

```bash
git clone https://github.com/ehmo/gum.git
cd gum/apps/gum
make build
./gum --version
```

Source builds report `dev` unless release ldflags inject a version string.

## Next

Run the [Quickstart](quickstart.md) after install. If an older unrelated `gum`
binary appears first on `PATH`, call the intended binary by absolute path or
adjust `PATH` before configuring agents.
