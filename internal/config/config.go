// Package config provides access to env variables with sane defaults
package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	Addr          string
	UploadDir     string
	MaxUploadSize int64 // bytes
}

const (
	defaultAddr          = ":8080"
	defaultUploadDir     = "uploads"
	defaultMaxUploadSize = int64(100 * 1024 * 1024) // 100 MB
)

// FromEnv builds configuration from environment variables with sane defaults.
func FromEnv() Config {
	cfg := Config{
		Addr:          defaultAddr,
		UploadDir:     defaultUploadDir,
		MaxUploadSize: defaultMaxUploadSize,
	}

	if v := os.Getenv("IMAGEDROP_ADDR"); v != "" {
		cfg.Addr = v
	}

	if v := os.Getenv("IMAGEDROP_UPLOAD_DIR"); v != "" {
		cfg.UploadDir = v
	}

	if v := os.Getenv("IMAGEDROP_MAX_UPLOAD_MB"); v != "" {
		mb, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("invalid IMAGEDROP_MAX_UPLOAD_MB=%q, using default %d MB", v, defaultMaxUploadSize/1024/1024)
		} else if mb > 0 {
			cfg.MaxUploadSize = int64(mb) * 1024 * 1024
		}
	}

	return cfg
}
