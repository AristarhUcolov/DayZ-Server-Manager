// Copyright (c) 2026 Aristarh Ucolov.

package terrain

import "errors"

var errLZO = errors.New("terrain: damaged compressed data")

// lzo1x unpacks LZO1X data from src into exactly n bytes and reports how
// many bytes of src it used. Bohemia's terrain files store their big arrays
// this way with no length in front: the decoder stops at the stream's own end
// marker, which tells where the next field starts.
func lzo1x(src []byte, n int) ([]byte, int, error) {
	out := make([]byte, 0, n)
	ip := 0
	in := func(k int) bool { return ip+k <= len(src) }
	// copyMatch copies length bytes from distance back (overlapping is the
	// point: a run of one byte repeats it).
	copyMatch := func(dist, length int) error {
		from := len(out) - dist
		if from < 0 || dist <= 0 || len(out)+length > n {
			return errLZO
		}
		for i := 0; i < length; i++ {
			out = append(out, out[from+i])
		}
		return nil
	}
	literals := func(k int) error {
		if !in(k) || len(out)+k > n {
			return errLZO
		}
		out = append(out, src[ip:ip+k]...)
		ip += k
		return nil
	}
	// extended reads a run length continued in zero bytes.
	extended := func(base int) (int, error) {
		t := 0
		for {
			if !in(1) {
				return 0, errLZO
			}
			if src[ip] != 0 {
				break
			}
			t += 255
			ip++
			if t > 1<<30 {
				return 0, errLZO
			}
		}
		t += base + int(src[ip])
		ip++
		return t, nil
	}

	const (
		stLoop = iota
		stFirstLiteralRun
		stMatch
		stMatchDone
		stMatchNext
	)
	if !in(1) {
		return nil, 0, errLZO
	}
	var t int
	state := stLoop
	if src[0] > 17 {
		t = int(src[0]) - 17
		ip = 1
		if t < 4 {
			state = stMatchNext
		} else {
			if err := literals(t); err != nil {
				return nil, 0, err
			}
			state = stFirstLiteralRun
		}
	}
	for {
		switch state {
		case stLoop:
			if !in(1) {
				return nil, 0, errLZO
			}
			t = int(src[ip])
			ip++
			if t >= 16 {
				state = stMatch
				continue
			}
			if t == 0 {
				var err error
				if t, err = extended(15); err != nil {
					return nil, 0, err
				}
			}
			if err := literals(t + 3); err != nil {
				return nil, 0, err
			}
			state = stFirstLiteralRun
		case stFirstLiteralRun:
			if !in(1) {
				return nil, 0, errLZO
			}
			t = int(src[ip])
			ip++
			if t >= 16 {
				state = stMatch
				continue
			}
			if !in(1) {
				return nil, 0, errLZO
			}
			dist := 1 + 0x0800 + (t >> 2) + int(src[ip])<<2
			ip++
			if err := copyMatch(dist, 3); err != nil {
				return nil, 0, err
			}
			state = stMatchDone
		case stMatch:
			switch {
			case t >= 64:
				if !in(1) {
					return nil, 0, errLZO
				}
				dist := 1 + (t>>2)&7 + int(src[ip])<<3
				ip++
				if err := copyMatch(dist, (t>>5)+1); err != nil {
					return nil, 0, err
				}
			case t >= 32:
				t &= 31
				if t == 0 {
					var err error
					if t, err = extended(31); err != nil {
						return nil, 0, err
					}
				}
				if !in(2) {
					return nil, 0, errLZO
				}
				dist := 1 + (int(src[ip])|int(src[ip+1])<<8)>>2
				ip += 2
				if err := copyMatch(dist, t+2); err != nil {
					return nil, 0, err
				}
			case t >= 16:
				dist := (t & 8) << 11
				t &= 7
				if t == 0 {
					var err error
					if t, err = extended(7); err != nil {
						return nil, 0, err
					}
				}
				if !in(2) {
					return nil, 0, errLZO
				}
				dist += (int(src[ip]) | int(src[ip+1])<<8) >> 2
				ip += 2
				if dist == 0 {
					// The end marker.
					if len(out) != n {
						return nil, 0, errLZO
					}
					return out, ip, nil
				}
				if err := copyMatch(dist+0x4000, t+2); err != nil {
					return nil, 0, err
				}
			default:
				if !in(1) {
					return nil, 0, errLZO
				}
				dist := 1 + (t >> 2) + int(src[ip])<<2
				ip++
				if err := copyMatch(dist, 2); err != nil {
					return nil, 0, err
				}
			}
			state = stMatchDone
		case stMatchDone:
			t = int(src[ip-2]) & 3
			if t == 0 {
				state = stLoop
				continue
			}
			state = stMatchNext
		case stMatchNext:
			if err := literals(t); err != nil {
				return nil, 0, err
			}
			if !in(1) {
				return nil, 0, errLZO
			}
			t = int(src[ip])
			ip++
			state = stMatch
		}
	}
}
