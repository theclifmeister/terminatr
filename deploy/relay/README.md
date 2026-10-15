# tm relay in Docker

The relay that links two tm servers (`tm relay serve`), as a container:
`ghcr.io/theclifmeister/tm-relay`, for amd64 and arm64, non-root (uid
65532). The full guide is docs/OPERATIONS.md, Running a relay.

Each release has these files as `tm-relay-docker.tar.gz`, with the
compose files pinned to that release:

```sh
curl -fLO https://github.com/theclifmeister/terminatr/releases/latest/download/tm-relay-docker.tar.gz
tar xzf tm-relay-docker.tar.gz && cd tm-relay-docker
```

Pick one:

- **An IP address, no domain** (`compose.selfsigned.yaml`): a NAS or home
  server on the LAN or a tailnet. The relay makes its own key; servers
  check it by its key.

  ```sh
  mkdir state && sudo chown 65532:65532 state
  echo RELAY_IP=192.168.1.20 > .env       # the address the servers reach
  docker compose -f compose.selfsigned.yaml up -d
  docker compose -f compose.selfsigned.yaml logs relay
  ```

  The log shows the relay's address with its pin
  (`wss://192.168.1.20:8443#sha256=…`): pair with it, `tm peer pair --relay
  <that address>`. Back up `state/`: it holds the key. Where `state/`
  belongs to your own user (Synology), add `RELAY_USER=1026:100` (your
  `id -u`:`id -g`) to `.env` instead of the `chown`.

- **A domain, certificate by ACME** (`compose.acme.yaml`, with Caddy):
  ports 80 and 443 open and the name's DNS pointing at the host.

  ```sh
  echo RELAY_HOST=relay.example.net > .env
  docker compose -f compose.acme.yaml up -d
  ```

- **A domain, your own certificate files** (`compose.yaml`): put
  `fullchain.pem` and `privkey.pem` in `certs/`, readable by uid 65532,
  then `docker compose up -d`.

Upgrade: put `TM_VERSION=<new tag>` in `.env` and run the same `up -d`;
servers reconnect by themselves. To build the image yourself instead of
pulling it, uncomment `build:` in the compose file and add `--build`
(`Dockerfile` checks the release binary against its `checksums.txt`).
