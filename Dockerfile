FROM golang:1.26 AS go-builder
WORKDIR /app
# Module proxy (proxy.golang.org is unreachable in some networks, e.g. mainland China).
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
ARG TARGETOS
ARG TARGETARCH
ARG BUILD_VERSION
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN set -eux; \
    GOOS="${TARGETOS:-$(go env GOOS)}"; \
    GOARCH="${TARGETARCH:-$(go env GOARCH)}"; \
    BUILD_VERSION_RESOLVED="${BUILD_VERSION:-}"; \
    if [ -z "${BUILD_VERSION_RESOLVED}" ] && [ -f VERSION ]; then BUILD_VERSION_RESOLVED="$(cat VERSION | tr -d "[:space:]")"; fi; \
    CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build -buildvcs=false -ldflags="-s -w -X whale2api/internal/version.BuildVersion=${BUILD_VERSION_RESOLVED}" -o /out/whale2api ./cmd/whale2api; \
    CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build -buildvcs=false -ldflags="-s -w" -o /out/poolui ./cmd/poolui

FROM busybox:1.36.1-musl AS busybox-tools

FROM debian:bookworm-slim AS runtime-base
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates gosu \
    && groupadd -r whale2api && useradd -r -g whale2api -d /app -s /usr/sbin/nologin ds2api \
    && mkdir -p /app/data /data && chown -R ds2api:whale2api /app /data \
    && rm -rf /var/lib/apt/lists/*
COPY --from=busybox-tools /bin/busybox /usr/local/bin/busybox
COPY docker/entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 5001
# Starts as root to fix bind-mounted /data ownership, then drops to ds2api (gosu).
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/whale2api"]

FROM runtime-base AS runtime-from-source
COPY --from=go-builder /out/whale2api /usr/local/bin/whale2api
COPY --from=go-builder /out/poolui /usr/local/bin/poolui

FROM runtime-from-source AS final
