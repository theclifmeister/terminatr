# tm relay serve in a small, non-root image (docs/OPERATIONS.md, Running a
# relay). It holds the released tm binary and nothing else: the download
# is checked against the release's checksums.txt, as scripts/install.sh
# does.
#
#   docker build -t tm-relay --build-arg TM_VERSION=v0.27.0 .
#   docker run -d --name tm-relay -p 443:8443 -v /srv/tm-relay/certs:/certs:ro tm-relay
#
# The relay needs a tm with `tm relay serve`; pin TM_VERSION to a tag
# rather than following latest. Images for amd64 and arm64.

FROM debian:12-slim AS fetch
ARG TM_VERSION=latest
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /x
RUN set -eu; \
    case "${TARGETARCH:-}" in amd64|arm64) ;; *) echo "terminatr has no Linux build for '${TARGETARCH:-}'" >&2; exit 1 ;; esac; \
    archive="tm_linux_${TARGETARCH}.tar.gz"; \
    if [ "$TM_VERSION" = latest ]; then \
      base=https://github.com/theclifmeister/terminatr/releases/latest/download; \
    else \
      base="https://github.com/theclifmeister/terminatr/releases/download/$TM_VERSION"; \
    fi; \
    curl -fsSLO "$base/$archive"; \
    curl -fsSLO "$base/checksums.txt"; \
    want=$(awk -v n="$archive" '{ f = $2; sub(/^\*/, "", f); if (f == n) { print tolower($1); exit } }' checksums.txt); \
    [ -n "$want" ] || { echo "checksums.txt has no sha256 for $archive" >&2; exit 1; }; \
    echo "$want  $archive" | sha256sum -c -; \
    tar -xzf "$archive" tm; \
    ./tm version

# glibc (tm's Linux builds need 2.28 or later) and nothing else; uid 65532.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=fetch /x/tm /usr/local/bin/tm
USER nonroot:nonroot
# 8443 inside, so no capability is needed: publish it as 443 (-p 443:8443).
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/tm", "relay", "serve"]
# Certificate files by mount, reloaded when they change (an ACME client
# renewing them). For ACME in front instead, see compose.acme.yaml.
CMD ["--listen", ":8443", "--cert", "/certs/fullchain.pem", "--key", "/certs/privkey.pem"]
