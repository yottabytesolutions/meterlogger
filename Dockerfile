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
    CGO_ENABLED=0 GOARCH=$TARGETARCH go build \
        -trimpath \
        -ldflags="-s -w -X main.BuildDate=$(date -u '+%Y-%m-%dT%H:%M:%SZ') -X main.CommitSHA=${GIT_SHA}" \
        -o /out/meterlogger \
        ./cmd/meterlogger

# --- Production Stage ---
# distroless/static includes ca-certificates, /etc/passwd with a nonroot user
# (uid 65532), and is on Docker Scout's approved-base-image list, which lets
# the supply-chain policies report a real result instead of "no data".
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build-env /out/meterlogger /meterlogger

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/meterlogger", "healthcheck"]

ENTRYPOINT ["/meterlogger"]
