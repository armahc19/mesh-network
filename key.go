package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func key() {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		panic(err)
	}
	fmt.Println("PSK (hex):", hex.EncodeToString(key))
}
