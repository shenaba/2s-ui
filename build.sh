#!/bin/sh

# Abort on the first failure. Both copy steps below clear their target
# before writing it, so a build that failed and was not noticed does not
# just skip an update -- it removes the previous one and then links a
# binary around whatever is left. For the subscriber dashboard that is the
# committed fallback page, which ships looking like a finished feature.
set -e

cd frontend
npm i
npm run build

cd ..
echo "Backend"

mkdir -p web/html
rm -fr web/html/*
cp -R frontend/dist/* web/html/

# Subscriber dashboard: served to browsers at the subscription URL. Built and
# embedded under sub/dashboard (go:embed). A minimal fallback index.html is
# committed so the package still compiles if this frontend hasn't been built.
echo "Subscriber dashboard"
cd frontend/subscriber
npm i
npm run build
cd ../..
rm -fr sub/dashboard/index.html sub/dashboard/assets
cp -R frontend/subscriber/dist/* sub/dashboard/

BUILD_TAGS="with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_musl,badlinkname,tfogo_checklinkname0,with_tailscale,with_cloudflared,with_openconnect,with_openvpn,with_usbip"
go build -ldflags '-w -s -checklinkname=0 -extldflags "-Wl,-no_warn_duplicate_libraries"' -tags "$BUILD_TAGS" -o sui main.go
