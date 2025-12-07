package server

import (
	"fmt"
	"net"
)

// DetectLANAddress returns a list of example URLs that can be used from other devices.
func DetectLANAddress(listenAddr string) ([]string, error) {
	// listenAddr is usually ":8080" or "0.0.0.0:8080"
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return nil, err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		// We need to discover a LAN interface
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return nil, err
		}
		var urls []string
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.IsLoopback() || !ip.IsPrivate() || ip.To4() == nil {
				continue
			}
			urls = append(urls, fmt.Sprintf("http://%s:%s/", ip.String(), port))
		}

		if len(urls) == 0 {
			// Fallback: 127.0.0.1 for local use
			urls = append(urls, fmt.Sprintf("http://127.0.0.1:%s/", port))
		}

		return urls, nil
	}

	return []string{fmt.Sprintf("http://%s:%s/", host, port)}, nil
}
