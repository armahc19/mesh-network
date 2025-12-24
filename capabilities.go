package main

import (
	"context"
	"log"
	"fmt"
	

	dht "github.com/libp2p/go-libp2p-kad-dht"
	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	 "github.com/libp2p/go-libp2p/core/peer"
)

func GetCapabilityKeys(res *ResourceCheckResultDHT) []string {
	var keys []string

	// Global membership
	keys = append(keys, "mesh-node-v1")

	// ---- RAM ----
	ramGB := res.RAMFree / (1024 * 1024 * 1024)
	switch {
	case ramGB >= 64:
		keys = append(keys, "mesh-ram-64gb")
	case ramGB >= 32:
		keys = append(keys, "mesh-ram-32gb")
	case ramGB >= 16:
		keys = append(keys, "mesh-ram-16gb")
	case ramGB >= 8:
		keys = append(keys, "mesh-ram-8gb")
	}

	// ---- DISK ----
	diskGB := res.DiskFree / (1024 * 1024 * 1024)
	switch {
	case diskGB >= 1000:
		keys = append(keys, "mesh-disk-1tb")
	case diskGB >= 500:
		keys = append(keys, "mesh-disk-500gb")
	case diskGB >= 100:
		keys = append(keys, "mesh-disk-100gb")
	}

	// ---- CPU ----
	keys = append(keys, fmt.Sprintf("mesh-cpu-%s", res.CPUArch))

	switch {
	case res.CPUCores >= 16:
		keys = append(keys, "mesh-cpu-16core")
	case res.CPUCores >= 8:
		keys = append(keys, "mesh-cpu-8core")
	case res.CPUCores >= 4:
		keys = append(keys, "mesh-cpu-4core")
	}

	// ---- GPU ----
	if res.GPU {
		keys = append(keys, "mesh-gpu-cuda")
	}

	return keys
}


// PublishCapabilities announces node capabilities into the DHT
func PublishCapabilities(ctx context.Context, kad *dht.IpfsDHT, keys []string) {
	for _, key := range keys {
		// Convert string → CID
		hash, err := mh.Sum([]byte(key), mh.SHA2_256, -1)
		if err != nil {
			log.Printf("Failed to hash key %s: %v", key, err)
			continue
		}
		c := cid.NewCidV1(cid.Raw, hash)

		// Provide to DHT
		err = kad.Provide(ctx, c, true)
		if err != nil {
			log.Printf("DHT: failed to provide key %s: %v", key, err)
		} else {
			log.Printf("DHT: provided capability key: %s", key)
		}
	}
}


// FindPeersByCapability returns peer IDs providing a capability

func FindPeersByCapability(ctx context.Context, kad *dht.IpfsDHT, key string) ([]peer.AddrInfo, error) {
    hash, err := mh.Sum([]byte(key), mh.SHA2_256, -1)
    if err != nil {
        return nil, err
    }
    c := cid.NewCidV1(cid.Raw, hash)

	providers, err := kad.FindProviders(ctx, c)
if err != nil {
    return nil, err
}

// Just return directly since it's already []peer.AddrInfo
return providers, nil

}
