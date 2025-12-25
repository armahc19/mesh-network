package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"

	dht "github.com/libp2p/go-libp2p-kad-dht"
)

// ServiceID is the unique hash of the application image (the "What")
type ServiceID string

// ServiceDefinition defines the requirements and identity of an app
type ServiceDefinition struct {
	ID           ServiceID `json:"id"`           // The Image Hash/Digest
	Name         string    `json:"name"`         // Human readable name
	InternalPort int       `json:"internal_port"` // Port the app listens on inside container
	Protocol     string    `json:"protocol"`      // "http", "tcp", etc.
}

// ServiceInstance defines a LIVE running service on the mesh (the "Where")
type ServiceInstance struct {
	Definition ServiceDefinition `json:"definition"`
	HostPeerID string            `json:"host_peer_id"` // The PeerID of the node running it
	MeshPort   int               `json:"mesh_port"`    // The dynamic port assigned by the host
	Status     string            `json:"status"`       // "starting", "running", "degraded"
}

// FullIdentity returns a string representation for logging
func (s *ServiceInstance) FullIdentity() string {
	return fmt.Sprintf("%s:%d (on %s)", s.Definition.ID, s.MeshPort, s.HostPeerID)
}

func CreateDefinitionFromRuntime(info RuntimeInfo, imageHash string, port int) ServiceDefinition {
	return ServiceDefinition{
		ID:           ServiceID(imageHash),
		Name:         info.Runtime + "-app",
		InternalPort: port,
		Protocol:     "http", // Defaulting to HTTP for MVP
	}
}

// GetImageHash fetches the unique SHA256 ID of a local Docker image
func GetImageHash(imageName string) (ServiceID, error) {
	// Executes: docker inspect --format='{{.Id}}' imageName
	cmd := exec.Command("docker", "inspect", "--format={{.Id}}", imageName)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %v", err)
	}

	// Docker returns hashes in format: sha256:7b92...
	rawHash := strings.TrimSpace(string(out))
	// Clean the prefix if you want just the hex string
	cleanHash := strings.TrimPrefix(rawHash, "sha256:")
	
	return ServiceID(cleanHash), nil
}


func RegisterLocalService(hID string, imageName string, externalPort int) (*ServiceInstance, error) {
	// 1. Get the Identity from the Image
	imgHash, err := GetImageHash(imageName)
	if err != nil {
		return nil, err
	}

	// 2. Define the Service (The "What")
	def := ServiceDefinition{
		ID:           imgHash,
		Name:         imageName,
		InternalPort: 80,      // The port inside the container
		Protocol:     "http",
	}

	// 3. Create the Instance (The "Where")
	instance := ServiceInstance{
		Definition: def,
		HostPeerID: hID,
		MeshPort:   externalPort, // The port mapped on your physical machine
		Status:     "running",
	}

	// 4. Save to local state
	MyHostedServices[imgHash] = instance
	
	fmt.Printf("✔ Registered Service Identity: %s\n", imgHash[:12])
	return &instance, nil
}


// ServiceIDToCID converts our hex image hash into a libp2p-compatible CID
func ServiceIDToCID(id ServiceID) (cid.Cid, error) {
    // 1. Clean the ID: Remove "sha256:" prefix if present
    hexStr := strings.TrimPrefix(string(id), "sha256:")

    // 2. Decode the raw hex into bytes
    rawBytes, err := hex.DecodeString(hexStr)
    if err != nil {
        return cid.Undef, fmt.Errorf("hex decode failed: %v", err)
    }

    // 3. Wrap it in a Multihash (SHA2-256 = 0x12, length = 32)
    // Using mh.Encode ensures the buffer has the correct length and prefix
    mhash, err := mh.Encode(rawBytes, mh.SHA2_256)
    if err != nil {
        return cid.Undef, fmt.Errorf("multihash encode failed: %v", err)
    }

    // 4. Create CID V1
    return cid.NewCidV1(cid.Raw, mhash), nil
}

func StartServiceAdvertisement(ctx context.Context, kadDHT *dht.IpfsDHT) {
    ticker := time.NewTicker(5 * time.Minute) // Advertise every 5 mins
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Loop through all services this node is currently hosting
            for id, instance := range MyHostedServices {
                c, err := ServiceIDToCID(id)
                if err != nil {
                    log.Printf("❌ Failed to create CID for service %s: %v", id, err)
                    continue
                }

                // Announce to the DHT that this node provides this Service CID
                // 'true' means we want to broadcast this to the network
                if err := kadDHT.Provide(ctx, c, true); err != nil {
                    log.Printf("❌ DHT: Failed to advertise service %s: %v", id, err)
                } else {
                    log.Printf("📢 DHT: Advertised service instance: %s", instance.FullIdentity())
                }
            }
        }
    }
}