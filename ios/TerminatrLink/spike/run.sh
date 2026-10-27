#!/bin/sh
# The phone link spike (T786) end to end: a server's real link (go test
# -run TestPhoneSpike… in internal/peer), through a self-signed relay on
# loopback (tm relay serve --self-signed's code, which that test runs;
# set TM_PHONE_SPIKE_RELAY for another), and TerminatrLink's spike tests
# as the phone, on this Mac or in the iOS Simulator.
#
#   spike/run.sh [mac|sim] [link|impostor] [client|server]
#
# The last makes the phone that end of the link's TLS; either, unset.
#
# sim needs a simulator device named by TM_SPIKE_SIM (default
# tm-link-spike): xcrun simctl create tm-link-spike <type> <runtime>.
set -eu
where=${1:-mac}
mode=${2:-link}
export TM_PHONE_SPIKE_ROLE=${3:-}
here=$(cd "$(dirname "$0")/.." && pwd)
repo=$(cd "$here/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/tm-link-spike.XXXXXX")
go_pid=
trap 'kill $go_pid 2>/dev/null || true' EXIT

case $mode in
link) gotest=TestPhoneSpikeLink ;;
impostor) gotest=TestPhoneSpikeImpostor ;;
*) echo "mode: link or impostor" >&2; exit 2 ;;
esac
(cd "$repo" && TM_PHONE_SPIKE_DIR=$work go test -count=1 -v -run "^$gotest\$" ./internal/peer) >"$work/go.log" 2>&1 &
go_pid=$!

case $where in
mac)
	(cd "$here" && TM_PHONE_SPIKE_DIR=$work TM_PHONE_SPIKE_MODE=$mode swift test --filter spike 2>&1) |
		grep -E 'spike|passed|failed|error' || true
	;;
sim)
	(cd "$here" && TEST_RUNNER_TM_PHONE_SPIKE_DIR=$work TEST_RUNNER_TM_PHONE_SPIKE_MODE=$mode TEST_RUNNER_TM_PHONE_SPIKE_ROLE=$TM_PHONE_SPIKE_ROLE \
		xcodebuild test -scheme TerminatrLink -destination "platform=iOS Simulator,name=${TM_SPIKE_SIM:-tm-link-spike}" \
		-only-testing:TerminatrLinkTests 2>&1) | grep -E 'spike|passed|failed|error:' || true
	;;
*) echo "where: mac or sim" >&2; exit 2 ;;
esac

wait $go_pid && status=0 || status=$?
go_pid=
echo "--- go end (internal/peer $gotest)"
cat "$work/go.log"
exit $status
