#!/bin/sh
# Reads changed file names on stdin and prints those that are code: all
# but the docs. Docs are docs/, site/ and the root-level *.md (README,
# CONTRIBUTING, SECURITY). Other Markdown is not: internal/skill/rules and
# internal/mod/bundled are go:embedded and tested, so a change to them
# must run the Go tests. Used by the `changes` job of ci.yml.
grep -vE '^(docs/|site/|[^/]+\.md$)' || true
