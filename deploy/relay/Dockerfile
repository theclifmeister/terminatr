# tm relay serve in a small, non-root image (docs/OPERATIONS.md, Running a
# relay). It holds the released tm binary and nothing else: the download
# is checked against the release's checksums.txt, as scripts/install.sh
# does.
#
#   docker build -t tm-relay --build-arg TM_VERSION=v0.27.0 .
#   docker run -d --name tm-relay -p 443:8443 -v /srv/tm-relay/certs:/certs:ro tm-relay
#
# TM_VERSION is required: a release tag with `tm relay serve`, never
# latest. The base images are pinned by digest (Dependabot moves them), so
# a tag changing upstream changes nothing here. Images for amd64 and arm64.
#
# Each release publishes this image, built from this file, as
# ghcr.io/theclifmeister/tm-relay:<tag> (and :latest); the compose files
# use it.

FROM debian:12-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3 AS fetch
ARG TM_VERSION
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /x
RUN set -eu; \
    case "${TM_VERSION:-}" in v[0-9]*) ;; *) echo "set --build-arg TM_VERSION to a release tag (vX.Y.Z), not '${TM_VERSION:-}'" >&2; exit 1 ;; esac; \
    case "${TARGETARCH:-}" in amd64|arm64) ;; *) echo "terminatr has no Linux build for '${TARGETARCH:-}'" >&2; exit 1 ;; esac; \
    archive="tm_linux_${TARGETARCH}.tar.gz"; \
    base="https://github.com/theclifmeister/terminatr/releases/download/$TM_VERSION"; \
    curl -fsSLO "$base/$archive"; \
    curl -fsSLO "$base/checksums.txt"; \
    want=$(awk -v n="$archive" '{ f = $2; sub(/^\*/, "", f); if (f == n) { print tolower($1); exit } }' checksums.txt); \
    [ -n "$want" ] || { echo "checksums.txt has no sha256 for $archive" >&2; exit 1; }; \
    echo "$want  $archive" | sha256sum -c -; \
    tar -xzf "$archive" tm; \
    ./tm version

# glibc (tm's Linux builds need 2.28 or later) and nothing else; uid 65532.
FROM gcr.io/distroless/cc-debian12:nonroot@sha256:9dac0a79194e45a7da0158a9c6da57b217585af0786db3845d1f0ec1a0dd182f
LABEL org.opencontainers.image.source="https://github.com/theclifmeister/terminatr" \
      org.opencontainers.image.description="terminatr relay (tm relay serve)" \
      org.opencontainers.image.licenses="MIT"
COPY --from=fetch /x/tm /usr/local/bin/tm
USER nonroot:nonroot
# The Go GC works harder near this, well before compose's mem_limit 256m.
ENV GOMEMLIMIT=200MiB
# 8443 inside, so no capability is needed: publish it as 443 (-p 443:8443).
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/tm", "relay", "serve"]
# Certificate files by mount, reloaded when they change (an ACME client
# renewing them). For ACME in front instead, see compose.acme.yaml.
CMD ["--listen", ":8443", "--cert", "/certs/fullchain.pem", "--key", "/certs/privkey.pem"]
