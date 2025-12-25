package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

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
    
    // You can also move your existing /mesh/resources/1.0.0 handler here later!
    log.Println("✅ P2P Stream Handlers registered.")
}
