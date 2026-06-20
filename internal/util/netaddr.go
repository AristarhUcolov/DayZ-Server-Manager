// Copyright (c) 2026 Aristarh Ucolov.
package util

import (
	"net"
	"sort"
)

// LANAddresses returns the machine's non-loopback IPv4 addresses, sorted with
// the most "LAN-looking" private ranges first (192.168.* / 10.* / 172.16-31.*).
// Used to tell the operator which address to type on a phone / other device
// when the panel is exposed on the LAN.
func LANAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifaces {
		// Skip down or loopback interfaces.
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue // IPv4 only — simpler to type, and what most LANs use
			}
			if ip4.IsLinkLocalUnicast() {
				continue // 169.254.* APIPA — not useful
			}
			out = append(out, ip4.String())
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return lanRank(out[i]) < lanRank(out[j])
	})
	return out
}

// lanRank orders private home/office ranges ahead of anything else.
func lanRank(ip string) int {
	switch {
	case hasPrefix(ip, "192.168."):
		return 0
	case hasPrefix(ip, "10."):
		return 1
	case hasPrefix(ip, "172."):
		return 2
	default:
		return 3
	}
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
