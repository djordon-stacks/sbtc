# sBTC signer libp2p OOM PoC

PoC for the vulnerability report "Regular user can OOM signers by flooding libp2p connections" (Immunefi report 82461). A single external host opens a flood of short-lived libp2p connections to one signer, each with a fresh peer id. The signer's `connection_limits` behaviour denies every connection, but the `bootstrap` behaviour — declared earlier in the `NetworkBehaviour` derive — has already inserted a permanent entry into its `connected_peers` map. The resulting `ListenFailure` event never reaches `peer_disconnected`, so the entry leaks forever and the signer's RSS climbs in a straight line until the kernel OOM-kills it.

## Files

- `docker/attack/Dockerfile` — two-stage Go build, ships a static `/usr/local/bin/flooder` binary on a slim debian base.
- `docker/attack/flooder.go` — the attacker. Mints a fresh libp2p identity (TCP + Noise + Yamux only — no QUIC/WS/relay so it stays cheap) for every dial, then dials `sbtc-signer-2`'s libp2p port using a peer ID derived from the victim's public devnet secp256k1 key. It never authenticates — the `connection_limits` behaviour denies it, but the `bootstrap` behaviour at `signer/src/network/libp2p/swarm.rs:56` already inserted a permanent entry into `connected_peers` (`signer/src/network/libp2p/bootstrap.rs:281-291`) — and the `ListenFailure` event the swarm emits never reaches `peer_disconnected`, so the entry leaks forever. 120 workers in a loop.
- `docker/attack/go.mod` — pins `go-libp2p` and `go-multiaddr`. `go mod tidy` resolves the rest inside the builder image.
- `docker/docker-compose.attack.yml` — overlay that adds the `sbtc-attacker` container under the `attack` profile and switches the three signers from fluentd to json-file logging so `docker logs` works.
- `attack.sh` — one-shot driver.

## How to run the PoC

From the repo root:

```bash
bash attack.sh
```

What it does:

1. Brings up the 3-signer devnet using the `default`, `bitcoin-mempool`, and `sbtc-signer` profiles from `docker/docker-compose.yml` plus the attack overlay (`docker compose -f docker/docker-compose.yml -f docker/docker-compose.attack.yml ... up -d --build`). The attacker is gated behind the `attack` profile, so it does NOT come up yet.
2. Polls `sbtc-signer-2`'s Prometheus endpoint (`localhost:9182/metrics`) until `peers_connected == 2` — i.e. the target has meshed with signers 1 and 3.
3. Prints the meshed baseline RSS.
4. Brings up `sbtc-attacker` under the `attack` profile. The attacker reads the victim's public devnet key from `TARGET_PRIVKEY_HEX` purely to derive the peer ID it will dial, then hammers `sbtc-signer-2:4122` with new identities.
5. Tails `sbtc-signer-2`'s RSS and the `peers_connected` gauge every 3 seconds. You'll see the gauge climb past 2 (the real signers), since every leaked entry also bumps the gauge — and RSS climb linearly until the kernel OOM-kills the container. Stop the watch loop with Ctrl+C.

Useful follow-ups while watching:

- `docker logs -f sbtc-attacker` — the attacker prints `STATS connected=N/2s failed-or-denied=M/2s` every 2 seconds. Once `max_pending_incoming` saturates, denials dominate; both still leak.
- `docker stats sbtc-signer-2` — live RSS.
- `docker exec sbtc-signer-2 curl -s localhost:9182/metrics | grep peers_connected` — leaked gauge.

To stop the attack but keep the leak in place (proving it doesn't reclaim): `docker rm -f sbtc-attacker`. To recover the signer: `docker restart sbtc-signer-2` — only a restart drops the leaked map.

## Stopping the PoC

- **Stop the watch loop**: Ctrl+C in the terminal running `attack.sh`. This only stops the RSS/gauge printer — the devnet and attacker keep running.
- **Stop the attacker but leave the devnet up** (useful for showing the leak doesn't reclaim after the attacker leaves):
  ```bash
  docker rm -f sbtc-attacker
  ```
- **Recover the victim signer** without tearing down the devnet:
  ```bash
  docker restart sbtc-signer-2
  ```
  Only a restart drops the leaked `connected_peers` map; stopping the attacker alone does not.
- **Tear everything down** (devnet + attacker + volumes):
  ```bash
  docker compose -f docker/docker-compose.yml -f docker/docker-compose.attack.yml \
    --profile default --profile bitcoin-mempool --profile sbtc-signer --profile attack \
    down -v
  ```
  Drop `-v` if you want to keep the postgres/bitcoin data volumes between runs.
