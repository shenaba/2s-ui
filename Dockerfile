FROM --platform=$BUILDPLATFORM node:alpine AS front-builder
WORKDIR /app
COPY frontend/ ./
RUN npm install && npm run build
RUN cd subscriber && npm install && npm run build

FROM golang:1.26-alpine AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
ENV GOARCH=$TARGETARCH

RUN apk upgrade --no-cache --scripts=no apk-tools && \
    apk add --no-cache \
    gcc \
    musl-dev \
    libc-dev \
    make \
    git \
    wget \
    unzip \
    bash \
    curl

ENV CC=gcc

RUN CRONET_ARCH="$TARGETARCH" && \
    CRONET_URL="https://github.com/SagerNet/cronet-go/releases/latest/download/libcronet-linux-${CRONET_ARCH}.so"; \
    echo "Downloading $CRONET_URL" && \
    wget -q -O ./libcronet.so "$CRONET_URL" && \
    chmod 755 ./libcronet.so

COPY . .
COPY --from=front-builder /app/dist/ /app/web/html/
# Overwrites the committed fallback index.html with the real dashboard.
COPY --from=front-builder /app/subscriber/dist/ /app/sub/dashboard/

RUN if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale,with_cloudflared,with_openconnect,with_openvpn,with_usbip" \
    -o sui main.go

FROM alpine
LABEL org.opencontainers.image.source="https://github.com/shenaba/2s-ui"
ENV TZ=Asia/Tehran
# Marks the container so the panel's self-update takes the Docker path: swap
# the binary in the writable layer, then re-exec the entrypoint (no systemd
# here). The update survives `docker restart` but recreating the container
# reverts to the image's version — pull a new image to stay in sync.
ENV S_UI_IN_DOCKER=1
WORKDIR /app
RUN set -ex && apk upgrade --no-cache --scripts=no apk-tools && \
    apk add --no-cache --upgrade bash ca-certificates nftables
COPY --from=backend-builder /app/sui /app/libcronet.so /app/
COPY --chmod=755 entrypoint.sh /app/
ENTRYPOINT [ "./entrypoint.sh" ]
