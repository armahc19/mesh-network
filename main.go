package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/network"
	pstore "github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"

	//	"github.com/libp2p/go-libp2p/core/host"
	//"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var bootstrapAddr = []string{
    "/ip4/10.221.123.1/tcp/4001/p2p/12D3KooWLVwiyp4SxfGkg1FNPdQc1veWZPhSCPNfbk4itoVWAaUj",
}

const DiscoveryNamespace = "ai-node"

// PSK for private network
var pskHex = "652c765126bb04a82bdadd7c5b1df9321c9ee679e0f9243a5ab27d275e097fe8"

func main() {
	// Parse command line flags
	port := flag.Int("port", 0, "Port to listen on (0 for random)")
	flag.Parse()

	ctx := context.Background()

	pskBytes, err := hex.DecodeString(pskHex)
	if err != nil {
		panic(err)
	}

	// Create connection manager
	cmgr, err := connmgr.NewConnManager(50, 100)
	if err != nil {
		panic(err)
	}

	// Create host with PSK + connection manager + listening port
	var opts []libp2p.Option
	opts = append(opts,
		libp2p.PrivateNetwork(pskBytes),
		libp2p.ConnectionManager(cmgr),
	)
	
	if *port != 0 {
		opts = append(opts, libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", *port)))
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Node ID: %s\n", h.ID())
	fmt.Printf("Listening addresses:\n")
	for _, addr := range h.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
	}

	// Create Kademlia DHT
	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		panic(err)
	}

	// Bootstrap the DHT
	if err = kadDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}

	// Wait a bit for DHT to initialize
	time.Sleep(2 * time.Second)

	// Get the actual listening address for this node
	var nodeAddr string
	for _, addr := range h.Addrs() {
		if addr.String() != "" {
			// Use IPv4 address
			ma, _ := multiaddr.NewMultiaddr(fmt.Sprintf("%s/p2p/%s", addr, h.ID()))
			nodeAddr = ma.String()
			fmt.Printf("Node address: %s\n", nodeAddr)
			break
		}
	}

	// If this is the first node (bootstrap), don't try to connect to itself
	// Otherwise, connect to the first node
	if *port != 4001 {
		// Connect to the bootstrap node (node 1)
		//bootstrapAddr := "/ip4/10.221.123.1/tcp/4001/p2p/12D3KooWLVwiyp4SxfGkg1FNPdQc1veWZPhSCPNfbk4itoVWAaUj"

		for _, addr := range bootstrapAddr {
			pi, err := pstore.AddrInfoFromString(addr)
			if err != nil {
				fmt.Printf("Error parsing bootstrap address: %v\n", err)
				continue
			}
			fmt.Printf("Connecting to bootstrap node: %s\n", pi.ID)
			if err := h.Connect(ctx, *pi); err != nil {
				fmt.Printf("Failed to connect to bootstrap: %v\n", err)
			} else {
				fmt.Printf("Connected to bootstrap node: %s\n", pi.ID)
			}
		}
	}

	// Start advertising and discovery
	routingDiscovery := routing.NewRoutingDiscovery(kadDHT)
	
	// Advertise this node
	go func() {
		for {
			ttl, err := routingDiscovery.Advertise(ctx, DiscoveryNamespace)
			if err != nil {
				fmt.Printf("Advertise failed: %v, retrying in 30s...\n", err)
			} else {
				fmt.Printf("Successfully advertised for TTL: %v\n", ttl)
			}
			time.Sleep(30 * time.Second)
		}
	}()

	// Discover peers
	go func() {
		for {
			fmt.Println("Looking for peers...")
			peerChan, err := routingDiscovery.FindPeers(ctx, DiscoveryNamespace)
			if err != nil {
				fmt.Printf("FindPeers error: %v\n", err)
				time.Sleep(10 * time.Second)
				continue
			}

			connectedCount := 0
			for pi := range peerChan {
				// Skip self and empty addresses
				if pi.ID == h.ID() || len(pi.Addrs) == 0 {
					continue
				}

				// Check if already connected
				if h.Network().Connectedness(pi.ID) == network.Connected {
					continue
				}

				fmt.Printf("Found peer: %s\n", pi.ID)
				
				// Try to connect
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := h.Connect(ctx, pi)
				cancel()
				
				if err != nil {
					fmt.Printf("Failed to connect to %s: %v\n", pi.ID, err)
				} else {
					fmt.Printf("Connected to peer: %s\n", pi.ID)
					connectedCount++
				}
			}
			
			if connectedCount > 0 {
				fmt.Printf("Connected to %d new peers\n", connectedCount)
			}
			
			time.Sleep(15 * time.Second)
		}
	}()

	// Also use util.Advertise for better discovery
	go util.Advertise(ctx, routingDiscovery, DiscoveryNamespace)

	// Print connected peers periodically
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			peers := h.Network().Peers()
			fmt.Printf("\n=== Connected Peers (%d) ===\n", len(peers))
			for _, p := range peers {
				conns := h.Network().ConnsToPeer(p)
				fmt.Printf("  %s (%d connections)\n", p, len(conns))
			}
			fmt.Println("=========================\n")
		}
	}
}