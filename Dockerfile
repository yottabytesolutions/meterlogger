ARG GIT_SHA="no-sha"
FROM --platform=$BUILDPLATFORM golang:1.26 AS build-env
WORKDIR /app
ARG GIT_SHA
ARG TARGETARCH
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-${TARGETARCH} \
    CGO_ENABLED=0 GOARCH=$TARGETARCH go build -ldflags="-s -w -X main.BuildDate=$(date -u '+%Y-%m-%dT%H:%M:%SZ') -X main.CommitSHA=${GIT_SHA}" -o ./ ./cmd/...
RUN addgroup --system apps && adduser --system --ingroup apps minion

# --- Production Stage ---
FROM scratch
COPY --from=build-env /etc/passwd /etc/passwd
COPY --from=build-env /etc/group /etc/group
COPY --from=build-env /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build-env /usr/share/zoneinfo/Europe/Amsterdam /usr/share/zoneinfo/Europe/Amsterdam

COPY --from=build-env /app/healthcheck /healthcheck
COPY --from=build-env /app/meterlogger /meterlogger

USER minion
ENV ZONEINFO=/opt/zoneinfo.zip

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/healthcheck"]

ENTRYPOINT ["/meterlogger"]