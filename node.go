package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"net"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Node struct {
	Ctx        context.Context
	Host       host.Host
	DHT        *dht.IpfsDHT
	advertOnce sync.Once
}

const TunnelProtocol = "/mesh/tunnel/1.0.0"


// OnServiceStarted is called by runtime_detect.go after a container is live
func (n *Node) OnServiceStarted(imageID string, port int) error {
	// 1. Register service identity locally (Phase 1)
	_, err := RegisterLocalService(n.Host.ID().String(), imageID, port)
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	// 2. Ensure background advertiser is running (Phase 2)
	n.advertOnce.Do(func() {
		go StartServiceAdvertisement(n.Ctx, n.DHT)
	})

	// 3. Immediate individual advertisement to the DHT
	c, _ := ServiceIDToCID(ServiceID(imageID))
	go func() {
		if err := n.DHT.Provide(n.Ctx, c, true); err != nil {
			log.Printf("❌ Initial DHT provide failed for %s: %v", imageID[:12], err)
		} else {
			fmt.Printf("📢 Mesh: Service %s is now live and discoverable!\n", imageID[:12])
		}
	}()

	return nil
}

// node.go

// FindService searches the DHT for providers of a specific Image Hash
func (n *Node) FindService(ctx context.Context, serviceID ServiceID) ([]ServiceInstance, error) {
	// 1. Convert Image Hash to CID
	c, err := ServiceIDToCID(serviceID)
	if err != nil {
		return nil, err
	}

	// 2. Search DHT for providers
	// FindProviders returns a slice of peer.AddrInfo
	providers, err := n.DHT.FindProviders(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("DHT search failed: %w", err)
	}

	var results []ServiceInstance

	// 3. For each provider found, get their live metadata
	for _, p := range providers {
		// Skip self
		if p.ID == n.Host.ID() {
			continue
		}

		// Connect to the provider to get their specific instance details
		// (This uses the protocol we built in the previous sessions)
		instance, err := n.RequestServiceMetadata(ctx, p.ID, serviceID)
		if err != nil {
			log.Printf("⚠️ Could not get metadata from %s: %v", p.ID, err)
			continue
		}
		results = append(results, *instance)
	}

	return results, nil
}

func (n *Node) RequestServiceMetadata(ctx context.Context, pid peer.ID, sid ServiceID) (*ServiceInstance, error) {
	// Protocol ID for service metadata
	const ServiceMetaProtocol = "/mesh/service-meta/1.0.0"

	s, err := n.Host.NewStream(ctx, pid, ServiceMetaProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	// Send the ServiceID we are asking about
	_, _ = s.Write([]byte(string(sid) + "\n"))

	// Read the response (JSON)
	var instance ServiceInstance
	if err := json.NewDecoder(s).Decode(&instance); err != nil {
		return nil, err
	}

	return &instance, nil
}

// node.go

// SetupHandlers registers all P2P protocol listeners for this node
func (n *Node) SetupHandlers() {
    // Handler for Phase 3: Service Metadata Discovery
    n.Host.SetStreamHandler("/mesh/service-meta/1.0.0", func(s network.Stream) {
        defer s.Close()
        
        // Read which ServiceID (Image Hash) the requester wants
        scanner := bufio.NewScanner(s)
        if scanner.Scan() {
            sid := ServiceID(strings.TrimSpace(scanner.Text()))
            
            // Look up in our local map (from Phase 1)
            // MyHostedServices should be accessible if it's in the same package
            if instance, ok := MyHostedServices[sid]; ok {
                // Send the JSON metadata back to the requester
                if err := json.NewEncoder(s).Encode(instance); err != nil {
                    log.Printf("Error encoding metadata: %v", err)
                }
            }
        }
    })

	// node.go (Inside SetupHandlers)

n.Host.SetStreamHandler(TunnelProtocol, func(s network.Stream) {
	// 1. Read the ServiceID header from the stream
	scanner := bufio.NewScanner(s)
	if !scanner.Scan() {
		log.Println("❌ Tunnel: Failed to read ServiceID header")
		s.Reset()
		return
	}
	sid := ServiceID(scanner.Text())

	// 2. Look up the local MeshPort for this ServiceID
	instance, ok := MyHostedServices[sid]
	if !ok {
		log.Printf("❌ Tunnel: Service %s not found on this host", sid[:12])
		s.Reset()
		return
	}

	// 3. Dial the dynamic local port
	targetAddr := fmt.Sprintf("127.0.0.1:%d", instance.MeshPort)
	conn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("❌ Tunnel: Failed to reach container at %s: %v", targetAddr, err)
		s.Reset()
		return
	}

	log.Printf("🚀 Tunnel Linked: Mesh -> Local Container %s (Port %d)", sid[:12], instance.MeshPort)

	// 4. Start the pipe
	go func() {
		defer s.Close()
		defer conn.Close()
		go io.Copy(s, conn)
		io.Copy(conn, s)
	}()
})
    
    // You can also move your existing /mesh/resources/1.0.0 handler here later!
    log.Println("✅ P2P Stream Handlers registered.")
}


// TunnelTraffic connects a local network connection to a remote P2P stream
// node.go

// UPDATE: Added 'sid ServiceID' to the parameters
func (n *Node) TunnelTraffic(ctx context.Context, target peer.ID, sid ServiceID, localConn net.Conn) {
    // Now it matches the 4 arguments: (ctx, target, sid, localConn)
    stream, err := n.Host.NewStream(ctx, target, TunnelProtocol)
    if err != nil {
        log.Printf("❌ Tunnel fail: %v", err)
        localConn.Close()
        return
    }

    // Write the Service ID as a header so the remote host knows which app to dial
    _, err = stream.Write([]byte(string(sid) + "\n"))
    if err != nil {
        log.Printf("❌ Failed to write tunnel header: %v", err)
        stream.Reset()
        return
    }

    // Start the bi-directional pipe
    go func() {
        defer stream.Close()
        defer localConn.Close()
        go io.Copy(stream, localConn)
        io.Copy(localConn, stream)
    }()
}
