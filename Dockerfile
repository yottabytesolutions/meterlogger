ARG GIT_SHA="no-sha"
FROM --platform=$BUILDPLATFORM golang:1.27.4 AS build-env
# Refresh the Debian trust store and remove eMudhra/emSign roots. The compromised
# intermediates were also cross-signed by Sectigo's AAA Certificate Services, so
# remove that trust anchor too: Go does not fetch CRLs or browser OneCRL updates.
# https://bugzilla.mozilla.org/show_bug.cgi?id=2083449
RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates \
    && sed -i -E '/emSign|AAA_Certificate_Services/s/^([^!])/!\1/' /etc/ca-certificates.conf \
    && update-ca-certificates --fresh \
    && openssl crl2pkcs7 -nocrl -certfile /etc/ssl/certs/ca-certificates.crt -out /tmp/ca.p7b \
    && openssl pkcs7 -in /tmp/ca.p7b -print_certs -noout -out /tmp/ca-subjects \
    && test -s /tmp/ca-subjects \
    && ! grep -Ei 'emSign|eMudhra|AAA Certificate Services' /tmp/ca-subjects \
    && rm -rf /var/lib/apt/lists/* /tmp/ca.p7b /tmp/ca-subjects
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
RUN addgroup --system apps && adduser --system --ingroup apps minion

# --- Production Stage ---
# scratch keeps Docker Scout's "no outdated/unapproved base images" policies
# satisfied (no base image to evaluate). Timezone data is embedded into the
# binary via the time/tzdata import so /usr/share/zoneinfo is not needed.
FROM scratch

COPY --from=build-env /etc/passwd /etc/passwd
COPY --from=build-env /etc/group /etc/group
COPY --from=build-env /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=build-env /out/meterlogger /meterlogger

USER minion

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/meterlogger", "healthcheck"]

ENTRYPOINT ["/meterlogger"]
