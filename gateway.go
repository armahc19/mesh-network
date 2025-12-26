package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type MeshGateway struct {
	P2PNode    *Node  // Link to your Node controller
	PublicPort string // e.g., ":80" or ":8080"
	Domain     string // e.g., "mesh.io"
}

// NewMeshGateway initializes the gateway settings
func NewMeshGateway(node *Node, port string, domain string) *MeshGateway {
	return &MeshGateway{
		P2PNode:    node,
		PublicPort: port,
		Domain:     domain,
	}
}

// ExtractServiceID parses the Image Hash from the incoming Host header
func (mg *MeshGateway) ExtractServiceID(host string) (ServiceID, error) {
	// Example: host = "sha256-ded7901.mesh.io"
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid host format: expected <hash>.<domain>")
	}

	// The first part is our Image Hash
	hashPart := parts[0]
	
	// Convert "sha256-abc..." back to "sha256:abc..." if needed
	id := strings.Replace(hashPart, "sha256-", "sha256:", 1)
	
	return ServiceID(id), nil
}

func (mg *MeshGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. Identify which service is being requested
    sid, err := mg.ExtractServiceID(r.Host)
    if err != nil {
        http.Error(w, "Invalid Mesh URL format", http.StatusBadRequest)
        return
    }

    // 2. Search the mesh for providers
    ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
    defer cancel()

    instances, err := mg.P2PNode.FindService(ctx, sid) // Changed serviceID to sid
    if err != nil || len(instances) == 0 {
        log.Printf("❌ Gateway: Service %s not found in mesh", string(sid)[:12])
        http.Error(w, "Service Not Found on Mesh", http.StatusNotFound)
        return
    }

    // 3. Pick the first available instance and get the Peer ID
    instance := instances[0]
    targetPeerID, err := peer.Decode(instance.HostPeerID) // Convert string back to peer.ID
    if err != nil {
        http.Error(w, "Invalid Peer ID found in mesh", http.StatusInternalServerError)
        return
    }

    log.Printf("✅ Gateway: Found provider %s for service %s", instance.HostPeerID, string(sid)[:12])

    // 4. TRIGGER THE PROXY (Replaces the fmt.Fprintf text)
    mg.ProxyToMesh(w, r, sid, targetPeerID)
}

// 5. Corrected Helper Signature (Added sid and used target type peer.ID)
func (mg *MeshGateway) ProxyToMesh(w http.ResponseWriter, r *http.Request, sid ServiceID, target peer.ID) {
    hijacker, ok := w.(http.Hijacker)
    if !ok {
        http.Error(w, "Webserver doesn't support hijacking", http.StatusInternalServerError)
        return
    }
    clientConn, _, err := hijacker.Hijack()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // This now matches the 4-argument function in node.go
    mg.P2PNode.TunnelTraffic(r.Context(), target, sid, clientConn)
}
