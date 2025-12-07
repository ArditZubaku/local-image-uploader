package main

import (
	"fmt"
	"log"
	"os"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/server"
)

func main() {
	cfg := config.FromEnv()

	srv := server.New(cfg)

	addrInfo, err := server.DetectLANAddress(cfg.Addr)
	if err != nil {
		log.Printf("Could not detect LAN address: %v", err)
		log.Printf("Server will listen on %s", cfg.Addr)
	} else {
		fmt.Println("Server listening on:")
		for _, a := range addrInfo {
			fmt.Printf("-> %s\n", a)
		}
		fmt.Println()
		fmt.Println("Open one of the URLs above in your phone browser (same Wi-Fi).")
	}

	if err := srv.Start(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
