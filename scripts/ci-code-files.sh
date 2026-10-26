#!/bin/sh
# Reads changed file names on stdin and prints those that are code for
# the Go jobs: all but the docs and the Mac app. Docs are docs/, site/
# and the root-level *.md (README, CONTRIBUTING, SECURITY). Other Markdown
# is not: internal/skill/rules and internal/mod/bundled are go:embedded
# and tested, so a change to them must run the Go tests. macos/ is Swift,
# built and tested by its own job (macos-app), which also checks its
# generated types against the app API. Used by the `changes` job of ci.yml.
grep -vE '^(docs/|site/|macos/|[^/]+\.md$)' || true
