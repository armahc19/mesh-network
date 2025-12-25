package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const ResourceProtocol = "/mesh/resources/1.0.0"

func RegisterResourceProtocol(h host.Host) {
	h.SetStreamHandler(ResourceProtocol, handleResourceRequest)
}

func handleResourceRequest(s network.Stream) {
	defer s.Close()
	res := CollectRuntimeResources()
	writer := bufio.NewWriter(s)
	if err := json.NewEncoder(writer).Encode(res); err != nil {
		log.Printf("Failed to send resource info: %v", err)
		return
	}
	writer.Flush()
}

func RequestResources(ctx context.Context, h host.Host, pid peer.ID) (*ResourceCheckResult, error) {
	s, err := h.NewStream(ctx, pid, ResourceProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	// Add a log here to prove you are receiving
    log.Printf("Requesting and waiting for data from peer: %s", pid)

	reader := bufio.NewReader(s)
	var res ResourceCheckResult
	if err := json.NewDecoder(reader).Decode(&res); err != nil {
		return nil, err
	}
	// Process data...
    log.Printf("Successfully RECEIVED resource data from peer: %s", pid)


	return &res, nil
}
