#!/usr/bin/env bash
# core/protocol/*/inbound.go are verbatim copies of sing-box's own inbound
# implementations, forked only so that UpdateUsers can be attached to them
# (see core/inbound_users.go). They will keep compiling after a sing-box bump
# while silently running the old implementation, so this compares them against
# the version currently in go.mod and fails on any drift.
#
# Run after bumping sing-box. If a file legitimately diverges, re-copy it from
# the module cache and re-apply the local change, then update EXPECT_DIFF below.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION=$(go list -m -f '{{.Version}}' github.com/sagernet/sing-box)
MODDIR=$(go list -m -f '{{.Dir}}' github.com/sagernet/sing-box)

if [ ! -d "$MODDIR" ]; then
  echo "sing-box module not in cache; run 'go mod download' first" >&2
  exit 1
fi

echo "comparing core/protocol/*/inbound.go against sing-box $VERSION"

# Lines each copy is expected to differ by, beyond the shared header comment.
#
# The six user-carrying protocols key their service by user name rather than by
# list position (Service[string], not Service[int]) -- see the header of any of
# those files. That patch is what these counts are: they are not a tolerance,
# they are the exact size of a change we made on purpose, so a bump that alters
# the file elsewhere still shows up. anytls has no user table and stays verbatim.
#
# vmess also carries 2 lines of import alias, since its own package is `vmess`.
#
# Re-copying after a sing-box bump will change these. Re-apply the patch, then
# put the new counts here -- and read the diff first rather than just pasting
# the number the script printed, which is how a real upstream change gets
# rubber-stamped into the expected total.
expect_diff() {
  case "$1" in
    hysteria) echo 28 ;;
    hysteria2) echo 28 ;;
    trojan) echo 25 ;;
    tuic) echo 26 ;;
    vless) echo 29 ;;
    vmess) echo 27 ;;
    *) echo 0 ;;
  esac
}

status=0
checked=0
for dir in core/protocol/*/; do
  proto=$(basename "$dir")
  local_file="$dir/inbound.go"
  upstream_file="$MODDIR/protocol/$proto/inbound.go"

  [ -f "$local_file" ] || continue
  checked=$((checked + 1))
  if [ ! -f "$upstream_file" ]; then
    echo "  $proto: FAIL - no upstream counterpart at protocol/$proto/inbound.go"
    status=1
    continue
  fi

  # Drop the local header comment block: everything before the package clause.
  actual=$(diff <(sed -n '/^package /,$p' "$local_file") \
                <(sed -n '/^package /,$p' "$upstream_file") \
             | grep -c '^[<>]' || true)
  expected=$(expect_diff "$proto")

  if [ "$actual" -eq "$expected" ]; then
    echo "  $proto: ok"
  else
    echo "  $proto: DRIFT - $actual differing lines, expected $expected"
    diff <(sed -n '/^package /,$p' "$local_file") \
         <(sed -n '/^package /,$p' "$upstream_file") | sed 's/^/      /' || true
    status=1
  fi
done

# An unmatched glob would run the loop zero times and exit 0, which reads as
# "everything is in sync" when it actually means the check found nothing.
if [ "$checked" -eq 0 ]; then
  echo "no inbound.go found under core/protocol/ -- has the layout changed?" >&2
  exit 1
fi

if [ "$status" -ne 0 ]; then
  echo
  echo "The copies no longer match sing-box $VERSION." >&2
  echo "Re-copy from $MODDIR/protocol/<proto>/inbound.go and re-apply local changes." >&2
fi
exit "$status"
