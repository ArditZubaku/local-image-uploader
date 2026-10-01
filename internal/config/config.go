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
	ShareDir      string // root directory exposed by the browse/download pages
	MaxUploadSize int64  // bytes
}

const (
	defaultAddr          = ":8080"
	defaultUploadDir     = "uploads"
	defaultMaxUploadSize = int64(0) // 0 = unlimited, bounded only by disk space
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

	// Defaults to UploadDir: the same folder you upload into is what you
	// browse/download from, unless pointed elsewhere explicitly.
	cfg.ShareDir = cfg.UploadDir
	if v := os.Getenv("IMAGEDROP_SHARE_DIR"); v != "" {
		cfg.ShareDir = v
	}

	if v := os.Getenv("IMAGEDROP_MAX_UPLOAD_MB"); v != "" {
		mb, err := strconv.Atoi(v)
		switch {
		case err != nil:
			log.Printf("invalid IMAGEDROP_MAX_UPLOAD_MB=%q, uploads are unlimited", v)
		case mb > 0:
			cfg.MaxUploadSize = int64(mb) * 1024 * 1024
		default:
			cfg.MaxUploadSize = 0
		}
	}

	return cfg
}
