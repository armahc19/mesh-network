package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
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

	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	 
)

var bootstrapAddr = []string{
    "/ip4/10.221.123.1/tcp/4001/p2p/12D3KooWLVwiyp4SxfGkg1FNPdQc1veWZPhSCPNfbk4itoVWAaUj",
}

const DiscoveryNamespace = "ai-node"

// PSK for private network
var pskHex = "652c765126bb04a82bdadd7c5b1df9321c9ee679e0f9243a5ab27d275e097fe8"

func main() {
	 // 1. Setup the log file at the very start of main()
	 f, err := os.OpenFile("mesh_node.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	 if err != nil {
		 fmt.Printf("Warning: couldn't create log file: %v\n", err)
	 } else {
		 defer f.Close()
		 // Tell the log package to write to the file, not the screen
		 log.SetOutput(f)
	 }
	 
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

	// ---- STEP 2: Detect & Publish Capabilities ----
	res := CollectRuntimeResources()
	capKeys := GetCapabilityKeys(res)
	
	// Log what capabilities we are publishing
	log.Printf("Publishing capability keys: %v", capKeys)
	
	PublishCapabilities(ctx, kadDHT, capKeys)
	


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
	// --- START ADVERTISING AND DISCOVERY (CLEAN VERSION) ---
	routingDiscovery := routing.NewRoutingDiscovery(kadDHT)
	
	// 1. Advertise (Only one loop, logging to file)
	go func() {
		for {
			ttl, err := routingDiscovery.Advertise(ctx, DiscoveryNamespace)
			if err != nil {
				log.Printf("Network: Advertise failed: %v", err)
			} else {
				log.Printf("Network: Successfully advertised (TTL: %v)", ttl)
			}
			time.Sleep(30 * time.Second)
		}
	}()

	// 2. Discover Peers (Logging to file)
	go func() {
		for {
			// This used to be fmt.Println, now it's silent
			log.Println("Network: Looking for peers...") 
			peerChan, err := routingDiscovery.FindPeers(ctx, DiscoveryNamespace)
			if err != nil {
				log.Printf("Network: FindPeers error: %v", err)
				time.Sleep(10 * time.Second)
				continue
			}

			for pi := range peerChan {
				if pi.ID == h.ID() || len(pi.Addrs) == 0 {
					continue
				}
				if h.Network().Connectedness(pi.ID) == network.Connected {
					continue
				}

				// Try to connect silently
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := h.Connect(ctx, pi)
				cancel()
				
				if err != nil {
					log.Printf("Network: Failed to connect to %s: %v", pi.ID, err)
				} else {
					log.Printf("Network: Connected to peer: %s", pi.ID)
				}
			}
			time.Sleep(15 * time.Second)
		}
	}()

	// Also use util.Advertise for better discovery
	go util.Advertise(ctx, routingDiscovery, DiscoveryNamespace)


	
    // Bootstrap DHT
    if err = kadDHT.Bootstrap(ctx); err != nil {
        panic(err)
    }

	// Step 4: Start background ticker for capability publishing + discovery
    go func() {
        ticker := time.NewTicker(10 * time.Minute)
        defer ticker.Stop()
        for range ticker.C {
            res := CollectRuntimeResources()
            keys := GetCapabilityKeys(res)

            // Publish keys
            for _, key := range keys {
                hash, _ := mh.Sum([]byte(key), mh.SHA2_256, -1)
                c := cid.NewCidV1(cid.Raw, hash)
                if err := kadDHT.Provide(ctx, c, true); err != nil {

                    log.Printf("DHT: failed to provide key %s: %v", key, err)
                } else {
                    log.Printf("DHT: provided key %s", key)
                }
            }

            // Discover peers
            for _, key := range keys {
                peerInfos, err := FindPeersByCapability(ctx, kadDHT, key)
                if err != nil {
                    log.Printf("Failed to find peers for %s: %v", key, err)
                    continue
                }
                for _, p := range peerInfos {
                    info, err := RequestResources(ctx, h, p.ID)
                    if err != nil {
                        log.Printf("Failed to get resources from %s: %v", p.ID, err)
                        continue
                    }
                    log.Printf("Peer %s live resources: %+v", p.ID, info)
                }
            }
        }
    }() // <- goroutine ends here
    // --------------------------


	 // 2. In your background loops, use log.Printf instead of fmt.Println
	 go func() {
        for {
            ttl, err := routingDiscovery.Advertise(ctx, DiscoveryNamespace)
            if err != nil {
                log.Printf("Network: Advertise failed: %v", err) // Goes to file
            } else {
                log.Printf("Network: Advertised successfully (TTL: %v)", ttl) // Goes to file
            }
            time.Sleep(30 * time.Second)
        }
    }()

	 // Move the peer list monitor to a background goroutine 
    // Now logging to file to keep the CLI menu clean
    go func() {
        ticker := time.NewTicker(20 * time.Second)
        for range ticker.C {
            peers := h.Network().Peers()
            // CHANGED: log.Printf writes to mesh_node.log instead of the screen
            log.Printf("Network Stats: Connected Peers: %d", len(peers))
        }
    }()
	
    // START THE INTERACTIVE UI (Mesh CLI)
    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Println("\nMesh CLI")
        fmt.Println("1. Publish (Deploy Application)")
        fmt.Println("2. Network Status")
		fmt.Println("3. Query Peers by Capability") // <--- new option
        fmt.Println("4. Exit")
        fmt.Print("Enter your choice: ")

        scanner.Scan()
        choiceStr := strings.TrimSpace(scanner.Text())
        choice, _ := strconv.Atoi(choiceStr)

        switch choice {
        case 1:
            // This function is defined in publish.go
            // It will now run while the P2P node is active in the background
            publishFlow(scanner) 
        case 2:
            peers := h.Network().Peers()
            fmt.Printf("\n--- Connected Peers (%d) ---\n", len(peers))
            for _, p := range peers {
                fmt.Printf(" Peer ID: %s\n", p)
            }
		case 3:
			fmt.Print("Enter capability key (e.g., mesh-gpu-cuda): ")
			scanner.Scan()
			key := strings.TrimSpace(scanner.Text())
		
			ctx := context.Background()
			peers, err := FindPeersByCapability(ctx, kadDHT, key)
			if err != nil {
				log.Printf("Failed to find peers for %s: %v", key, err)
				continue
			}
		
			for _, pi := range peers { // pi is peer.AddrInfo
				// FIX: Pass pi.ID instead of the whole struct pi
				info, err := RequestResources(ctx, h, pi.ID) 
				if err != nil {
					log.Printf("Failed to get resources from %s: %v", pi.ID, err)
					continue
				}
				log.Printf("Peer %s live resources: %+v", pi.ID, info)
			}
        case 4:
            fmt.Println("Shutting down node...")
            h.Close()
            return
        default:
            fmt.Println("Invalid choice.")
        }
    }
}