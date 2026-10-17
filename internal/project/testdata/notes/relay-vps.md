---
name: relay-vps
description: The relay server on the VPS: how it runs, where its logs are, how to update it
metadata:
  type: reference
---
# Relay on the VPS

The relay runs as a systemd service (tm-relay.service) on a small Ubuntu VPS behind Caddy for TLS.
Logs: journalctl -u tm-relay. Update: copy the new binary, then systemctl restart tm-relay.
Paired servers pin the relay's key; a new key means pairing again.
