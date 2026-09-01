#!/usr/bin/env bash
# The files under core/protocol/*/ are verbatim copies of sing-box's own
# implementations, forked only so that UpdateUsers can be attached to them (see
# core/inbound_users.go). They will keep compiling after a sing-box bump while
# silently running the old implementation, so this compares them against the
# version currently in go.mod and fails on any drift.
#
# Every .go file in a copy directory is checked, not just inbound.go: a protocol
# whose implementation is spread over more than one file (hysteria's quic.go)
# would otherwise sit there unwatched, which is the exact failure this script
# exists to catch. users.go is ours and has no upstream counterpart, so it is
# skipped by name.
#
# Run after bumping sing-box. If a file legitimately diverges, re-copy it from
# the module cache and re-apply the local change, then update expect_diff below.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION=$(go list -m -f '{{.Version}}' github.com/sagernet/sing-box)
MODDIR=$(go list -m -f '{{.Dir}}' github.com/sagernet/sing-box)

if [ ! -d "$MODDIR" ]; then
  echo "sing-box module not in cache; run 'go mod download' first" >&2
  exit 1
fi

echo "comparing core/protocol/ against sing-box $VERSION"

# Files that are ours rather than copies, so there is nothing upstream to
# compare them with.
LOCAL_ONLY="users.go"

# Lines each copy is expected to differ by, beyond the shared header comment,
# keyed by "<protocol>/<file>".
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
    hysteria/inbound.go) echo 28 ;;
    hysteria2/inbound.go) echo 28 ;;
    trojan/inbound.go) echo 25 ;;
    tuic/inbound.go) echo 26 ;;
    vless/inbound.go) echo 29 ;;
    vmess/inbound.go) echo 27 ;;
    *) echo 0 ;;
  esac
}

status=0
checked=0
for dir in core/protocol/*/; do
  proto=$(basename "$dir")
  for local_file in "$dir"*.go; do
    [ -f "$local_file" ] || continue
    file=$(basename "$local_file")
    case " $LOCAL_ONLY " in
      *" $file "*) continue ;;
    esac

    name="$proto/$file"
    upstream_file="$MODDIR/protocol/$proto/$file"
    checked=$((checked + 1))
    if [ ! -f "$upstream_file" ]; then
      echo "  $name: FAIL - no upstream counterpart at protocol/$name"
      echo "      (if this file is ours, add it to LOCAL_ONLY in this script)"
      status=1
      continue
    fi

    # Drop the local header comment block: everything before the package clause.
    actual=$(diff <(sed -n '/^package /,$p' "$local_file") \
                  <(sed -n '/^package /,$p' "$upstream_file") \
               | grep -c '^[<>]' || true)
    expected=$(expect_diff "$name")

    if [ "$actual" -eq "$expected" ]; then
      echo "  $name: ok"
    else
      echo "  $name: DRIFT - $actual differing lines, expected $expected"
      diff <(sed -n '/^package /,$p' "$local_file") \
           <(sed -n '/^package /,$p' "$upstream_file") | sed 's/^/      /' || true
      status=1
    fi
  done
done

# An unmatched glob would run the loop zero times and exit 0, which reads as
# "everything is in sync" when it actually means the check found nothing.
if [ "$checked" -eq 0 ]; then
  echo "no .go files found under core/protocol/ -- has the layout changed?" >&2
  exit 1
fi

if [ "$status" -ne 0 ]; then
  echo
  echo "The copies no longer match sing-box $VERSION." >&2
  echo "Re-copy from $MODDIR/protocol/<proto>/ and re-apply local changes." >&2
fi
exit "$status"
