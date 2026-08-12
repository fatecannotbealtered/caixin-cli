#!/bin/sh
# check-clean.sh — fail when a tracked file sits at the repository root without
# being part of the declared skeleton.
#
# This is the guard that would have caught the scraped page assets (app.js,
# lg.html, r.json, ...) that once shipped at the root: a debug capture is
# exactly the kind of file that lands there and nowhere else. POSIX sh only —
# git ls-files plus a case allowlist, no ripgrep.
#
# A tracked file deleted from the working tree does not count as an offender:
# locally that is a deletion awaiting commit, and in CI a tracked file always
# exists after checkout, so the guard loses nothing there.

set -eu

status=0
for file in $(git ls-files | grep -v /); do
	[ -e "$file" ] || continue
	case "$file" in
	.gitattributes | .gitignore | .golangci.yml | .goreleaser.yml | .npmrc | \
		AGENTS.md | AGENTS_zh.md | CHANGELOG.md | CODE_OF_CONDUCT.md | CODE_OF_CONDUCT_zh.md | \
		CONTRIBUTING.md | CONTRIBUTING_zh.md | LICENSE | Makefile | NOTICE.md | NOTICE_zh.md | \
		README.md | README_zh.md | SECURITY.md | SECURITY_zh.md | \
		changelog.go | go.mod | go.sum | package.json | package-lock.json) ;;
	*)
		echo "check-clean: unexpected tracked root file: $file" >&2
		status=1
		;;
	esac
done

if [ "$status" -ne 0 ]; then
	echo "check-clean: remove the files above or add them to the allowlist in scripts/check-clean.sh" >&2
fi
exit $status
