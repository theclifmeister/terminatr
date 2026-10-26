#!/bin/sh
# Notarises one zip, disk image or package with Apple's notary service
# and waits for the verdict. sign.sh (tm) and app.sh (the Mac app) run it:
#
#   scripts/release/notarize.sh <file> <name>
#
#   TM_NOTARY_KEY     path to the App Store Connect API key (.p8)
#   TM_NOTARY_KEY_ID  its id
#   TM_NOTARY_ISSUER  the App Store Connect issuer id
#
# It exits nonzero unless Apple accepted the file, after printing Apple's
# log of a rejection. Stapling is the caller's: only bundles, disk images
# and packages take a ticket.
set -eu
file=${1:?usage: notarize.sh <file> <name>}
name=${2:?usage: notarize.sh <file> <name>}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# --wait exits nonzero on anything but Accepted; --timeout keeps a stuck
# submission from using the job's whole budget.
rc=0
xcrun notarytool submit "$file" \
	--key "${TM_NOTARY_KEY:?}" --key-id "${TM_NOTARY_KEY_ID:?}" --issuer "${TM_NOTARY_ISSUER:?}" \
	--wait --timeout 30m >"$tmp/notary.log" 2>&1 || rc=$?
cat "$tmp/notary.log"
if [ "$rc" -ne 0 ] || ! grep -q 'status: Accepted' "$tmp/notary.log"; then
	# A rejection says only "Invalid"; the reason is in Apple's log.
	id=$(grep -oE '[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}' "$tmp/notary.log" | head -1)
	[ -z "$id" ] || xcrun notarytool log "$id" \
		--key "$TM_NOTARY_KEY" --key-id "$TM_NOTARY_KEY_ID" --issuer "$TM_NOTARY_ISSUER" || true
	echo "notarize: $name was not notarised" >&2
	exit 1
fi
echo "notarize: $name notarised"
