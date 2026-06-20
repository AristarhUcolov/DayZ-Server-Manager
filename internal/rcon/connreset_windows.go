// Copyright (c) 2026 Aristarh Ucolov.
//go:build windows

package rcon

import (
	"net"
	"syscall"
	"unsafe"
)

var (
	ws2_32       = syscall.NewLazyDLL("ws2_32.dll")
	procWSAIoctl = ws2_32.NewProc("WSAIoctl")
)

// SIO_UDP_CONNRESET control code.
const sioUDPConnReset = 0x9800000C

// disableConnReset turns OFF the Windows-only behaviour where a connected UDP
// socket's recv starts failing with WSAECONNRESET ("forcibly closed") after a
// prior send elicited an ICMP "port unreachable". Against a local BattlEye RCon
// port this otherwise kills the read loop right after login — so every command
// then times out and the panel reconnects in a loop (a new "RCon admin logged
// in" every few seconds). No-op / not compiled on other platforms.
func disableConnReset(conn *net.UDPConn) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		var flag uint32 // 0 == FALSE == disable the reset behaviour
		var bytesReturned uint32
		_, _, _ = procWSAIoctl.Call(
			fd,
			uintptr(sioUDPConnReset),
			uintptr(unsafe.Pointer(&flag)),
			4, // sizeof(flag)
			0, // out buffer
			0, // out buffer size
			uintptr(unsafe.Pointer(&bytesReturned)),
			0, // overlapped
			0, // completion routine
		)
	})
}
