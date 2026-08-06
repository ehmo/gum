---
title: Install
description: "Install gum from release archives, the install script, Homebrew, or source."
---

# Install

`gum` publishes release archives for common operating systems and CPU
architectures. Download the archive for your platform from the GitHub releases
page, verify it with `checksums.txt`, then put the binary on your `PATH`.

```bash
tar -xzf gum_<version>_<os>_<arch>.tar.gz
install -m 0755 gum ~/.local/bin/gum
gum --version
```

## Install script

After a public release exists, the install script can fetch the matching archive
and install the binary:

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | bash
```

Pin a version with `GUM_VERSION`:

```bash
GUM_VERSION=v1.0.0 \
  curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | bash
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
