# sBTC signer libp2p OOM PoC

A single host floods one signer with short-lived libp2p connections, each using a fresh peer id. `connection_limits` denies them, but `bootstrap`, which is declared earlier in the `NetworkBehaviour` derive, has already recorded each peer in its unbounded `connected_peers` map. Denied connections emit `ListenFailure`, which `bootstrap` ignores, so the entries are never cleaned up. The signer's resident set size, or RSS, is the physical memory it occupies; that climbs steadily until the kernel OOM-kills it.

## Files

- `docker/attack/Dockerfile` — two-stage Go build producing a static `flooder` binary.
- `docker/attack/flooder.go` — dials `sbtc-signer-2`'s libp2p port with a fresh identity per connection, using only TCP, Noise, and Yamux; the peer id to dial is derived from the victim's public devnet key. With `ATTACKER_PRIVKEY_HEX` set it reuses one fixed identity instead. 120 workers.
- `docker/attack/go.mod` — pins `go-libp2p` and `go-multiaddr`; `go mod tidy` resolves the rest in the build image.
- `docker/docker-compose.attack.yml` — adds `sbtc-attacker` under the `attack` profile and switches the signers to json-file logging so `docker logs` works.
- `attack.sh` — one-shot driver.

## Run

From the repo root:

```bash
bash attack.sh
```

It:

1. Brings up the 3-signer devnet plus the attack overlay. The attacker is gated behind the `attack` profile, so it stays down.
2. Waits until `sbtc-signer-2` reports `peers_connected == 2`, then prints its baseline RSS.
3. Starts `sbtc-attacker`, which floods `sbtc-signer-2:4122`.
4. Tails the victim's RSS and `peers_connected` every 3s. RSS climbs to OOM; the gauge climbs past 2 as leaked entries accumulate. Ctrl+C stops the watch loop.

While watching:

- `docker logs --follow sbtc-attacker` — per-2s `STATS connected/failed-or-denied` counts.
- `docker stats sbtc-signer-2` — live RSS.
- `docker exec sbtc-signer-2 curl --silent localhost:9182/metrics | grep peers_connected` — leaked gauge.

## Stop

- **Watch loop**: Ctrl+C. This leaves the devnet and attacker running.
- **Attacker, keep the leak**: `docker rm --force sbtc-attacker` — the leaked memory stays, proving it isn't reclaimed.
- **Recover the signer**: `docker restart sbtc-signer-2`. Only a restart drops the leaked map.
- **Tear down everything**:
  ```bash
  docker compose --file docker/docker-compose.yml --file docker/docker-compose.attack.yml \
    --profile default --profile bitcoin-mempool --profile sbtc-signer --profile attack \
    down --volumes
  ```
  Drop `--volumes` to keep data volumes.
