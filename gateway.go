package main

import (
	"context"
	"io"
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
// gateway.go

func (mg *MeshGateway) ExtractServiceID(host string) (ServiceID, error) {
	// 1. Get the subdomain (e.g., "ded7901... .mesh.io")
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid host format")
	}

	// 2. Identify the hash part
	hashPart := strings.TrimSpace(parts[0])

	// 3. NORMALIZE: Ensure it exactly matches what the CLI provides
	// If your CLI search uses raw hex, we strip "sha256-" if the user typed it
	cleanID := strings.TrimPrefix(hashPart, "sha256-")
	cleanID = strings.TrimPrefix(cleanID, "sha256:")

	log.Printf("🌐 Gateway normalized search key: [%s]", cleanID)
	return ServiceID(cleanID), nil
}

func (mg *MeshGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Identify which service is being requested
	sid, err := mg.ExtractServiceID(r.Host)
	if err != nil {
		http.Error(w, "Invalid Mesh URL format", http.StatusBadRequest)
		return
	}

	// 2. Search the mesh for providers
	// FIX: Use context.Background() instead of r.Context()
	// This prevents the search from being killed by the incoming HTTP request context
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
        http.Error(w, "Hijacking not supported", 500)
        return
    }

    clientConn, _, err := hijacker.Hijack()
    if err != nil {
        log.Printf("❌ Hijack failed: %v", err)
        http.Error(w, err.Error(), 500)
        return
    }
    defer clientConn.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    stream, err := mg.P2PNode.Host.NewStream(ctx, target, TunnelProtocol)
    if err != nil {
        log.Printf("❌ Stream creation failed: %v", err)
        return
    }
    defer stream.Close()

    // 1. Write ServiceID header
    log.Printf("🔍 Writing ServiceID: %s", string(sid)[:12])
    if _, err := stream.Write([]byte(string(sid) + "\n")); err != nil {
        log.Printf("❌ Failed to write ServiceID: %v", err)
        return
    }
    
    // 2. Log the HTTP request we're about to forward
    log.Printf("🔍 HTTP Request to forward:")
    log.Printf("  Method: %s", r.Method)
    log.Printf("  URL: %s", r.URL.String())
    log.Printf("  Proto: %s", r.Proto)
    log.Printf("  Host: %s", r.Host)
    log.Printf("  ContentLength: %d", r.ContentLength)
    
    // 3. Write the HTTP request to the stream
    // Start with request line
    requestLine := fmt.Sprintf("%s %s %s\r\n", r.Method, r.URL.RequestURI(), r.Proto)
    log.Printf("🔍 Writing request line: %s", strings.TrimSpace(requestLine))
    if _, err := stream.Write([]byte(requestLine)); err != nil {
        log.Printf("❌ Failed to write request line: %v", err)
        return
    }
    
    // Write headers with detailed logging
    log.Printf("🔍 Writing headers...")
    log.Printf("🔍 Original r.Header has %d keys:", len(r.Header))
    for key, values := range r.Header {
        log.Printf("  [%s]: %v", key, values)
    }
    
    // Track what we write
    headersWritten := []string{}
    for key, values := range r.Header {
        for _, value := range values {
            headerLine := fmt.Sprintf("%s: %s\r\n", key, value)
            headersWritten = append(headersWritten, fmt.Sprintf("%s: %s", key, value))
            if _, err := stream.Write([]byte(headerLine)); err != nil {
                log.Printf("❌ Failed to write header %s: %v", key, err)
                return
            }
        }
    }
    
    log.Printf("🔍 Headers written (%d):", len(headersWritten))
    for _, h := range headersWritten {
        log.Printf("  %s", h)
    }
    
    // Check if Host header was written
    hostWritten := false
    for _, h := range headersWritten {
        if strings.HasPrefix(strings.ToLower(h), "host:") {
            hostWritten = true
            log.Printf("✅ Host header found in written headers: %s", h)
            break
        }
    }
    if !hostWritten {
        log.Printf("⚠️ WARNING: Host header was NOT written!")
        // Force write Host header
        hostHeader := fmt.Sprintf("Host: %s\r\n", r.Host)
        if _, err := stream.Write([]byte(hostHeader)); err != nil {
            log.Printf("❌ Failed to write missing Host header: %v", err)
            return
        }
        log.Printf("✅ Manually added Host header: %s", strings.TrimSpace(hostHeader))
    }
    
    // End of headers
    if _, err := stream.Write([]byte("\r\n")); err != nil {
        log.Printf("❌ Failed to write header terminator: %v", err)
        return
    }
    
    log.Printf("🔍 Headers written, checking for body...")
    
    // Write body if present
    var bodyBytes int64 = 0
    if r.Body != nil {
        log.Printf("🔍 Copying request body...")
        bodyBytes, err = io.Copy(stream, r.Body)
        if err != nil {
            log.Printf("❌ Failed to copy body: %v", err)
            return
        }
        log.Printf("🔍 Copied %d bytes of body", bodyBytes)
    }
    
    log.Printf("🔍 HTTP request forwarded (%d header lines, %d body bytes)", len(headersWritten), bodyBytes)
    
    // Log the complete raw request for debugging
    log.Printf("🔍 COMPLETE REQUEST SENT:")
    log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
    for _, h := range headersWritten {
        log.Printf("%s", h)
    }
    log.Printf("")
    
    log.Printf("🔍 Starting bidirectional copy...")
    
    // 4. Start bidirectional copy
    done := make(chan struct{}, 2)
    
    go func() {
        n, err := io.Copy(stream, clientConn)
        log.Printf("🔍 Client->Stream copy done: %d bytes, err=%v", n, err)
        done <- struct{}{}
    }()
    
    go func() {
        n, err := io.Copy(clientConn, stream)
        log.Printf("🔍 Stream->Client copy done: %d bytes, err=%v", n, err)
        done <- struct{}{}
    }()
    
    // Wait for first completion
    <-done
    log.Printf("🔍 Gateway: Proxy complete")
}
