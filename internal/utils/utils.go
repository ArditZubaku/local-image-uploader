// Package utils offers utils like closing or logging on a resource
package utils

import (
	"fmt"
	"io"
	"log"
)

func CloseOrLog(closer io.Closer, resource string) {
	if err := closer.Close(); err != nil {
		log.Printf("Failed to close %s", resource)
	}
}

// FormatBytes renders a byte count as a short human-readable size (e.g. "1.2 GB").
func FormatBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(n)

	i := 0
	for size >= 1024 && i < len(units)-1 {
		size /= 1024
		i++
	}

	if i == 0 {
		return fmt.Sprintf("%d %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", size, units[i])
}
