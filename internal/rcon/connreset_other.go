// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package rcon

import "net"

// disableConnReset is a no-op outside Windows (SIO_UDP_CONNRESET is a Windows
// winsock quirk).
func disableConnReset(_ *net.UDPConn) {}
