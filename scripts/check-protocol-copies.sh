#!/usr/bin/env bash
# The files under core/protocol/*/ are verbatim copies of sing-box's own
# implementations, forked so that UpdateUsers can be attached to them (see
# core/inbound_users.go) -- or, for naive, so that its X-Forwarded-For can be
# judged (#279). They will keep compiling after a sing-box bump while
# silently running the old implementation, so this compares them against the
# version currently in go.mod and fails on any drift.
#
# Every .go file in a copy directory is checked, not just inbound.go: a protocol
# whose implementation is spread over more than one file (hysteria's quic.go)
# would otherwise sit there unwatched, which is the exact failure this script
# exists to catch. users.go and forwarded.go are ours and have no upstream
# counterpart, so they are skipped by name, as is any _test.go.
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
LOCAL_ONLY="users.go forwarded.go"

# Protocol directories that are ours as a whole: upstream sing-box has no such
# protocol, so nothing in them is a copy. mieru (issue #288) is built on the
# mieru library, not on any sing-box file.
LOCAL_DIRS="mieru"

# Lines each copy is expected to differ by, beyond the shared header comment,
# keyed by "<protocol>/<file>". These are not a tolerance: each is the exact
# size of changes we made on purpose, so a bump that alters the file anywhere
# else still shows up. Six changes make them up.
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
#    so that these counts stay this small -- see core/usersession. naive, the
#    eighth copy, is not among them: it is copied for change 6 alone. anytls costs
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
# 5. The three copies with a V2Ray transport (trojan, vless, vmess) build it
#    through newServerTransport (in users.go) rather than
#    v2ray.NewServerTransport, so that X-Forwarded-For is believed only from a
#    peer with a reason to send it (#279) -- see core/transport. That is 3
#    lines in each: the changed call, and the v2ray import it no longer needs.
#
# 6. naive serves its own http.Server, with the inbound itself as the handler,
#    so there is no transport to build through core/transport: the copy reads
#    the client through n.source (in forwarded.go) rather than
#    sHttp.SourceAddress, so that X-Forwarded-For is judged by the same rule
#    (#279). That is 2 lines, the one changed line; the HTTP/3 server
#    sing-box's naive/quic installs on its own package is hooked up from
#    forwarded.go, and inbound_conn.go is untouched.
#
# Re-copying after a sing-box bump will change these. Re-apply all six,
# then put the new counts here -- and read the diff first rather than just
# pasting the number the script printed, which is how a real upstream change
# gets rubber-stamped into the expected total.
expect_diff() {
  case "$1" in
    anytls/inbound.go) echo 7 ;;
    hysteria/inbound.go) echo 31 ;;
    hysteria2/inbound.go) echo 31 ;;
    naive/inbound.go) echo 2 ;;
    trojan/inbound.go) echo 35 ;;
    tuic/inbound.go) echo 29 ;;
    vless/inbound.go) echo 39 ;;
    vmess/inbound.go) echo 37 ;;
    *) echo 0 ;;
  esac
}

status=0
checked=0
for dir in core/protocol/*/; do
  proto=$(basename "$dir")
  case " $LOCAL_DIRS " in
    *" $proto "*) continue ;;
  esac
  for local_file in "$dir"*.go; do
    [ -f "$local_file" ] || continue
    file=$(basename "$local_file")
    case " $LOCAL_ONLY " in
      *" $file "*) continue ;;
    esac
    # Tests are ours: sing-box ships none of the copied files' tests.
    case "$file" in
      *_test.go) continue ;;
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

# core/transport is not a copy either: it serves sing-box's own HTTP transport
# servers, and does what their Serve and NewServer do around the handler --
# the http.Server's timeouts and contexts, the TLS listener and ALPN, h2c, the
# gRPC server options -- itself, along with the dispatch by transport type
# that v2ray.NewServerTransport does. Those go stale as silently, and a
# server that started reading the client somewhere new would go unnoticed
# too, so the files are pinned the same way: when one changes, read it
# against core/transport, carry over whatever matters, then update the sum.
transport_sha256() {
  case "$1" in
    transport/v2ray/transport.go) echo 3ff90cfde30a7b25db84174f61715c54b1a6f1242ac823469e877c655a971189 ;;
    transport/v2ray/grpc.go) echo e3dd0de0fa2ed3cb2d7f15d63a9b5e83da1447d06620c9131dd01d32217c26a6 ;;
    transport/v2ray/grpc_lite.go) echo f22bfbf382628e98d4cdfcbc1da44334e4c60431cab02aba05512f3a0ce13a58 ;;
    transport/v2raywebsocket/server.go) echo 8d2e6f59b28c095fe2e9de0cb61b69f6b1a28bc0f0e00a3b055c198da6b4e1cd ;;
    transport/v2rayhttp/server.go) echo 3d1e7f7ed90dcc488e7c2b31fd19adc08f6b60062d40e5f24693bc5463072b9d ;;
    transport/v2rayhttpupgrade/server.go) echo db30a71f1f6d5ba3553ba8d603d6be673c8ba355c9e660c0730dcd0aa6bded95 ;;
    transport/v2raygrpclite/server.go) echo ecc97d0761309f544b0e6bc9aaad6a7f7e1026d245e6071bff737dcf3be145e0 ;;
    transport/v2raygrpc/server.go) echo 56c006e3543faca3a5fc597ec0de1b107e366b7cc797e03c0440c2401772238e ;;
  esac
}

echo "checking upstream files mirrored by core/transport"
transport_status=0
for name in transport/v2ray/transport.go transport/v2ray/grpc.go transport/v2ray/grpc_lite.go \
            transport/v2raywebsocket/server.go transport/v2rayhttp/server.go \
            transport/v2rayhttpupgrade/server.go transport/v2raygrpclite/server.go \
            transport/v2raygrpc/server.go; do
  upstream_file="$MODDIR/$name"
  if [ ! -f "$upstream_file" ]; then
    echo "  $name: FAIL - gone from sing-box $VERSION"
    transport_status=1
    continue
  fi
  actual=$(sha256_of "$upstream_file")
  if [ "$actual" = "$(transport_sha256 "$name")" ]; then
    echo "  $name: ok"
  else
    echo "  $name: CHANGED - sha256 $actual"
    transport_status=1
  fi
done
# Which headers those servers take the client from is decided in sing, not
# sing-box: sHttp.SourceAddress. core/transport strips only X-Forwarded-For,
# so a sing that started reading another header (X-Real-IP, Forwarded) would
# have it believed from anyone again. core/transport's tests send the usual
# ones and would catch that too; this says where to look.
SING_MODDIR=$(go list -m -f '{{.Dir}}' github.com/sagernet/sing)
sing_addr="$SING_MODDIR/protocol/http/addr.go"
if [ ! -f "$sing_addr" ]; then
  echo "  sing protocol/http/addr.go: FAIL - gone from sing"
  transport_status=1
elif [ "$(sha256_of "$sing_addr")" = 44913275e6fe8e4c7dbfc3fc791ea01c0b5538d6f887d45f0697f5a19ae6e8a8 ]; then
  echo "  sing protocol/http/addr.go: ok"
else
  echo "  sing protocol/http/addr.go: CHANGED - sha256 $(sha256_of "$sing_addr")"
  transport_status=1
fi
if [ "$transport_status" -ne 0 ]; then
  echo
  echo "Upstream changed files that core/transport mirrors." >&2
  echo "Read them in $MODDIR against core/transport, then update transport_sha256." >&2
  status=1
fi
exit "$status"
