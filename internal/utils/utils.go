// Package utils offers utils like closing or logging on a resource
package utils

import (
	"io"
	"log"
)

func CloseOrLog(closer io.Closer, resource string) {
	if err := closer.Close(); err != nil {
		log.Printf("Failed to close %s", resource)
	}
}
