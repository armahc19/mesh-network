package main

import (
	"context"
	"errors"
	"fmt"
	"time"
	//"log"
	pstore "github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/host"
	"os/exec"
)

// scheduler.go
func SelectBestNode(peers []pstore.AddrInfo, h host.Host, ctx context.Context) (pstore.ID, error) {
	var bestNode pstore.ID
	var highestScore float64 = -1

	for _, pi := range peers {
		if pi.ID == h.ID() { continue }

		stats, err := RequestResources(ctx, h, pi.ID)
		if err != nil { continue }

		// Simple Scoring: (RAM_MB * 0.5) + (Cores * 0.5)
		score := (float64(stats.RAMFree) / 1024 / 1024 * 0.5) + (float64(stats.CPUCores) * 0.5)

		if score > highestScore {
			highestScore = score
			bestNode = pi.ID
		}
	}

	if highestScore == -1 {
		return "", errors.New("no healthy nodes found")
	}
	return bestNode, nil
}

// scheduler.go

func SchedulerChecker(ctx context.Context, node *Node, requiredSize uint64, serviceID string) (pstore.ID, error) {
	// 1. Define the tier based on image size for more accurate discovery
	// If image > 500MB, we look for 1GB nodes. Otherwise, we look for the broad capability.
	searchKey := "mesh-capable" 
	if requiredSize > (500 * 1024 * 1024 * 1024) { // > 500GB
		searchKey = "mesh-disk-1tb"
	} else if requiredSize > (100 * 1024 * 1024 * 1024) { // > 100GB
		searchKey = "mesh-disk-500gb"
	} else if requiredSize > (500 * 1024 * 1024) { // > 500MB
		searchKey = "mesh-disk-100gb"
	}
	
	fmt.Printf("🎯 Scheduling: Searching DHT for key [%s] to fit %d bytes...\n", searchKey, requiredSize)

	// 2. Increase the timeout. DHT walks in 2025 can be slow on private networks.
	searchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// 3. Find Peers
	peers, err := FindPeersByCapability(searchCtx, node.DHT, searchKey)
	
	// Fallback: If the specific tier search fails, try the broad "ai-node" namespace 
	// which is used in your main.go Advertise function.
	if err != nil || len(peers) == 0 {
		fmt.Println("⚠️ Specific tier not found. Falling back to broad discovery...")
		peers, err = FindPeersByCapability(searchCtx, node.DHT, "ai-node")
	}

	if len(peers) == 0 {
		// Final Check: Check connected peers directly even if DHT is blank
		directPeers := node.Host.Network().Peers()
		if len(directPeers) > 0 {
			fmt.Printf("🔗 Found %d connected peers via direct mesh. Querying them...\n", len(directPeers))
			for _, pID := range directPeers {
				peers = append(peers, node.Host.Peerstore().PeerInfo(pID))
			}
		} else {
			return "", fmt.Errorf("❌ No nodes found in DHT or direct connections")
		}
	}

	// 4. Interview the candidates for exact resource matches
	var bestPeer pstore.ID
	var maxFreeRAM uint64 = 0

	for _, pi := range peers {
		if pi.ID == node.Host.ID() {
			continue 
		}

		// Use the protocol you defined in main.go to get live stats
		stats, err := RequestResources(searchCtx, node.Host, pi.ID)
		if err != nil {
			continue
		}

		// Logic: ImageSize + 256MB RAM Buffer
		buffer := uint64(256 * 1024 * 1024)
		if stats.DiskFree > requiredSize && stats.RAMFree > (requiredSize + buffer) {
			if stats.RAMFree > maxFreeRAM {
				maxFreeRAM = stats.RAMFree
				bestPeer = pi.ID
			}
		}
	}

	if bestPeer == "" {
		return "", fmt.Errorf("insufficient resources found on discovered nodes")
	}

	return bestPeer, nil
}
/*
func SchedulerChecker(ctx context.Context, node *Node, requiredSize uint64, serviceID string, needsGPU bool, requiredCores int) (peer.ID, error) {
	// 1. DYNAMIC SEARCH KEY SELECTION
	// We build a list of potential search keys, from most specific to least specific.
	var searchTiers []string

	// ---- GPU Priority ----
	if needsGPU {
		searchTiers = append(searchTiers, "mesh-gpu-cuda")
	}

	// ---- RAM Tiers (based on image size + 256MB buffer) ----
	requiredRAM := (requiredSize / (1024 * 1024 * 1024)) + 1 // Est. GB
	if requiredRAM >= 64 {
		searchTiers = append(searchTiers, "mesh-ram-64gb")
	} else if requiredRAM >= 32 {
		searchTiers = append(searchTiers, "mesh-ram-32gb")
	} else if requiredRAM >= 16 {
		searchTiers = append(searchTiers, "mesh-ram-16gb")
	}

	// ---- CPU Tiers ----
	if requiredCores >= 16 {
		searchTiers = append(searchTiers, "mesh-cpu-16core")
	} else if requiredCores >= 8 {
		searchTiers = append(searchTiers, "mesh-cpu-8core")
	}

	// ---- Disk Tiers ----
	if requiredSize > (500 * 1024 * 1024 * 1024) { // > 500GB
		searchTiers = append(searchTiers, "mesh-disk-1tb")
	} else if requiredSize > (100 * 1024 * 1024 * 1024) { // > 100GB
		searchTiers = append(searchTiers, "mesh-disk-500gb")
	} else {
		searchTiers = append(searchTiers, "mesh-disk-100gb")
	}

	// Always add the universal fallback key
	searchTiers = append(searchTiers, "mesh-node-v1")

	// 2. SEARCH THE DHT
	var peers []peer.AddrInfo
	var err error
	
	// We try the keys in order of importance
	for _, key := range searchTiers {
		fmt.Printf("🔍 Searching DHT for nodes with capability: %s\n", key)
		searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		peers, err = FindPeersByCapability(searchCtx, node.DHT, key)
		cancel()

		if len(peers) > 0 {
			fmt.Printf("✅ Found %d candidate nodes in tier [%s]\n", len(peers), key)
			break
		}
	}

	if len(peers) == 0 {
		return "", fmt.Errorf("❌ No mesh nodes found matching hardware requirements")
	}

	// 3. LIVE RESOURCE VERIFICATION
	var bestPeer peer.ID
	var maxFreeRAM uint64 = 0

	for _, pi := range peers {
		if pi.ID == node.Host.ID() { continue }

		stats, err := RequestResources(ctx, node.Host, pi.ID)
		if err != nil { continue }

		// Hard verification against all keys
		if stats.DiskFree > requiredSize && 
		   stats.RAMFree > (requiredSize + (256 * 1024 * 1024)) &&
		   stats.CPUCores >= requiredCores {
			
			// Selection: Find the node with the most "headroom"
			if stats.RAMFree > maxFreeRAM {
				maxFreeRAM = stats.RAMFree
				bestPeer = pi.ID
			}
		}
	}

	if bestPeer == "" {
		return "", fmt.Errorf("❌ Candidates found, but live resources are currently at capacity")
	}

	return bestPeer, nil
}
*/


// seed.go (Sender Logic)
// seed.go (Corrected Sender Logic)
func BuildAndSeedContainer(ctx context.Context, h host.Host, target pstore.ID, imageID string, port int) error {
	// Use the consistent protocol string you registered in main.go
	stream, err := h.NewStream(ctx, target, "/mesh/deploy-container/1.0.0")
	if err != nil {
		return err
	}
	defer stream.Close()

	// 1. Create a metadata header (ID and Port)
	// We send this as the first line so the receiver knows how to 'docker run'
	header := fmt.Sprintf("%s:%d\n", imageID, port)
	_, err = stream.Write([]byte(header))
	if err != nil {
		return fmt.Errorf("failed to send metadata: %v", err)
	}

	// 2. Stream the container image directly using 'docker save'
	fmt.Printf("📦 Streaming container image [%s] to %s...\n", imageID, target)
	saveCmd := exec.Command("docker", "save", imageID)
	saveCmd.Stdout = stream // Direct pipe from Docker to the Mesh Network
	
	if err := saveCmd.Run(); err != nil {
		return fmt.Errorf("streaming failed: %v", err)
	}

	fmt.Println("✅ Container seeded successfully.")
	return nil
}

