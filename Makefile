SHELL := /bin/bash

.PHONY: gum-build docs-commands docs-services docs-site docs-check public-check

gum-build:
	$(MAKE) -C apps/gum build

docs-commands: gum-build
	node scripts/gen-command-reference.mjs

docs-services: gum-build
	node scripts/gen-service-reference.mjs

docs-site: docs-commands docs-services
	node scripts/build-docs-site.mjs

docs-check: docs-site
	node scripts/check-docs-site.mjs

public-check:
	scripts/check-public-release-contract.py
