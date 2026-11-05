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
# keyed by "<protocol>/<file>". These are not a tolerance: each is the exact
# size of changes we made on purpose, so a bump that alters the file anywhere
# else still shows up. Four changes make them up.
#
# 1. The six user-carrying protocols key their service by user name rather than
#    by list position (Service[string], not Service[int]) -- see the header of
#    any of those files. vmess also carries 2 lines of import alias, since its
#    own package is `vmess`. anytls has no user table and is untouched by this.
#
# 2. Every copy installs the user-session registry by wrapping its router, which
#    is one added line, identical in all seven:
#
#      inbound.router = withUserSessions(inbound.router)
#
#    Everything that line reaches lives in users.go, which is exempt below. The
#    hook is a wrapper rather than a field plus a block in each handler exactly
#    so that these counts stay this small -- see core/usersession. anytls costs
#    3 instead of 1: its session is held in NewConnection, which the router
#    never sees, so that one call is redirected through users.go as well.
#
# 3. The three QUIC copies (hysteria, hysteria2, tuic) close their sessions
#    gracefully -- see core/quicgrace. That is 2 lines in each: the service's
#    TLSConfig goes through gracefulTLS (defined in users.go), a changed line
#    rather than a new one. The other half -- the service closing before the
#    listener whose socket the CONNECTION_CLOSE has to go out on -- used to be
#    a reordered Close and is now upstream's own order: since 1.14.3 Start
#    registers scope.Add(listener.Close) before scope.Add(service.Close), and
#    the scope runs its cleanups in reverse. A re-copy that ever sees those two
#    swapped upstream has to swap them back here, and nothing below would count
#    it as drift, which is why core/quic_close_test.go exists.
#
# 4. The four TCP copies (anytls, trojan, vless, vmess) read a PROXY protocol
#    header from a front on the same host (#260) -- see core/proxyheader. That
#    is 6 lines in each copy that has a transport: a proxyHdr field, the
#    request taken out of the listen options before the listener is built,
#    the listener's ConnectionHandler and the transport's Serve argument
#    going through helpers in users.go (two changed lines, so 2 each). anytls
#    has no transport and costs 4. The QUIC copies are untouched: a front
#    that routes by SNI cannot route QUIC, so there is nothing to read.
#
# Re-copying after a sing-box bump will change these. Re-apply all four,
# then put the new counts here -- and read the diff first rather than just
# pasting the number the script printed, which is how a real upstream change
# gets rubber-stamped into the expected total.
expect_diff() {
  case "$1" in
    anytls/inbound.go) echo 7 ;;
    hysteria/inbound.go) echo 31 ;;
    hysteria2/inbound.go) echo 31 ;;
    trojan/inbound.go) echo 32 ;;
    tuic/inbound.go) echo 29 ;;
    vless/inbound.go) echo 36 ;;
    vmess/inbound.go) echo 34 ;;
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

# core/box.go and core/managers.go are not copies -- the managers had to be
# rewritten to support Remove and late starts, which 1.14.3 took out -- so a
# line diff against upstream says nothing useful about them. They do mirror
# what these upstream files do at start (the stage order, the scope teardown,
# the outbound dependency order, the default outbound), and would go stale
# exactly as silently as the copies above. So the upstream files they were last checked against are pinned
# by checksum: when one changes, read it against core/box.go and
# core/managers.go, carry over whatever matters, then update the sum here.
pinned_sha256() {
  case "$1" in
    box.go) echo ef74d46689ef631dfb57872e4feb9991d40480213f4ac8aaa757ab42977c3671 ;;
    adapter/lifecycle.go) echo 3229c6760a18010397d04a985bef7da832855e3d5d138498aceab99f3647bfac ;;
    adapter/inbound/manager.go) echo 86a590a31281399a376aef4caf2d59bb1287f3a38c4a561a9982cf8d6f3c2576 ;;
    adapter/outbound/manager.go) echo 531429c3a382fb6a50bd854d956de59b5ab4676dedd532a03af592d4e0e867ae ;;
    adapter/endpoint/manager.go) echo 27f6fe25e0c635ad17ad91ebb2f7728ea010cce9ed0e6242f5b49fec2589183b ;;
    adapter/service/manager.go) echo 14446e05ea83f34b9f20fb3aed9b6be55b2097f11c998cd5d2bcdc798ff6e372 ;;
  esac
}

# macOS ships shasum rather than sha256sum. Read from stdin either way: given a
# path, sha256sum escapes its output line with a leading backslash when the path
# has one, as it does on Windows.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum < "$1" | cut -d' ' -f1
  else
    shasum -a 256 < "$1" | cut -d' ' -f1
  fi
}

echo "checking upstream files mirrored by core/box.go and core/managers.go"
pinned_status=0
for name in box.go adapter/lifecycle.go adapter/inbound/manager.go adapter/outbound/manager.go adapter/endpoint/manager.go adapter/service/manager.go; do
  upstream_file="$MODDIR/$name"
  if [ ! -f "$upstream_file" ]; then
    echo "  $name: FAIL - gone from sing-box $VERSION"
    pinned_status=1
    continue
  fi
  actual=$(sha256_of "$upstream_file")
  if [ "$actual" = "$(pinned_sha256 "$name")" ]; then
    echo "  $name: ok"
  else
    echo "  $name: CHANGED - sha256 $actual"
    pinned_status=1
  fi
done
if [ "$pinned_status" -ne 0 ]; then
  echo
  echo "Upstream changed files that core/box.go and core/managers.go mirror." >&2
  echo "Read them in $MODDIR against those two, then update pinned_sha256." >&2
  status=1
fi
exit "$status"
