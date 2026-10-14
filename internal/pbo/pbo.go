// Copyright (c) 2026 Aristarh Ucolov.
//
// Package pbo reads Bohemia's PBO archives (the @mod/addons/*.pbo files) far
// enough to get the addon configs out of them: the file table, plain and
// LZSS-compressed entries, and binarized configs (config.bin, rap.go).
// Nothing is ever written.
package pbo

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	methodPlain      = 0x00000000
	methodCompressed = 0x43707273 // "Cprs"
	methodProduct    = 0x56657273 // "Vers"
	methodEncrypted  = 0x456e6372 // "Encr"
)

// Entry is one file inside a PBO.
type Entry struct {
	Name     string // path inside the archive, backslash-separated as stored
	Method   uint32
	Original uint32 // unpacked size (compressed entries)
	Size     uint32 // stored size
	Offset   int64  // where its data starts in the archive
}

// File is an opened PBO: its properties (prefix, …) and file table.
type File struct {
	Path       string
	Properties map[string]string
	Entries    []Entry
}

// maxEntries bounds a corrupt or hostile header.
const maxEntries = 200000

// Open reads the PBO's header (not the data).
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(f, 64<<10)
	out := &File{Path: path, Properties: map[string]string{}}
	var pos int64
	readZ := func() (string, error) {
		s, err := r.ReadString(0)
		if err != nil {
			return "", err
		}
		pos += int64(len(s))
		if len(s) > 1024 {
			return "", errors.New("pbo: name too long")
		}
		return s[:len(s)-1], nil
	}
	readU32 := func() (uint32, error) {
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		pos += 4
		return binary.LittleEndian.Uint32(b[:]), nil
	}
	first := true
	for {
		name, err := readZ()
		if err != nil {
			return nil, fmt.Errorf("pbo %s: header: %w", path, err)
		}
		var h [5]uint32
		for i := range h {
			if h[i], err = readU32(); err != nil {
				return nil, fmt.Errorf("pbo %s: header: %w", path, err)
			}
		}
		method, original, size := h[0], h[1], h[4]
		if name == "" && first && method == methodProduct {
			// Product entry: key/value strings up to an empty one.
			for {
				k, err := readZ()
				if err != nil {
					return nil, fmt.Errorf("pbo %s: properties: %w", path, err)
				}
				if k == "" {
					break
				}
				v, err := readZ()
				if err != nil {
					return nil, fmt.Errorf("pbo %s: properties: %w", path, err)
				}
				out.Properties[strings.ToLower(k)] = v
			}
			first = false
			continue
		}
		first = false
		if name == "" {
			break // end of the file table
		}
		out.Entries = append(out.Entries, Entry{Name: name, Method: method, Original: original, Size: size})
		if len(out.Entries) > maxEntries {
			return nil, fmt.Errorf("pbo %s: too many entries", path)
		}
	}
	// Data follows the header, in table order.
	off := pos
	for i := range out.Entries {
		out.Entries[i].Offset = off
		off += int64(out.Entries[i].Size)
	}
	if off > st.Size() {
		return nil, fmt.Errorf("pbo %s: truncated (%d bytes of data, file is %d)", path, off, st.Size())
	}
	return out, nil
}

// Find returns the entries whose base name (any folder) equals name, case
// insensitively — "config.bin" finds "config.bin" and "data\config.bin".
func (p *File) Find(name string) []Entry {
	var out []Entry
	for _, e := range p.Entries {
		base := e.Name
		if i := strings.LastIndexAny(base, `\/`); i >= 0 {
			base = base[i+1:]
		}
		if strings.EqualFold(base, name) {
			out = append(out, e)
		}
	}
	return out
}

// maxRead bounds what Read unpacks (configs are small; this is not a
// general-purpose extractor).
const maxRead = 64 << 20

// Read returns an entry's content, unpacking LZSS-compressed data.
func (p *File) Read(e Entry) ([]byte, error) {
	switch e.Method {
	case methodPlain, methodCompressed:
	case methodEncrypted:
		return nil, errors.New("pbo: encrypted entry")
	default:
		// Some tools store 0 for "plain" and garbage for unknown; treat
		// unknown as plain only when sizes agree.
		if e.Original != 0 && e.Original != e.Size {
			return nil, fmt.Errorf("pbo: unknown packing method %#x", e.Method)
		}
	}
	if e.Size > maxRead || e.Original > maxRead {
		return nil, errors.New("pbo: entry too large")
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, e.Size)
	if _, err := f.ReadAt(data, e.Offset); err != nil {
		return nil, err
	}
	if e.Method == methodCompressed && e.Original > 0 && e.Original != e.Size {
		return lzss(data, int(e.Original))
	}
	return data, nil
}

// lzss unpacks Bohemia's LZSS: a flag byte announces eight items, a set bit
// is a literal byte, a clear bit a back-reference of 2 bytes (12-bit
// distance, 4-bit length + 3); a distance reaching before the start yields
// spaces. A 4-byte checksum follows (not checked).
func lzss(in []byte, outLen int) ([]byte, error) {
	out := make([]byte, 0, outLen)
	i := 0
	for len(out) < outLen {
		if i >= len(in) {
			return nil, errors.New("pbo: compressed data ends early")
		}
		flags := in[i]
		i++
		for bit := 0; bit < 8 && len(out) < outLen; bit++ {
			if flags&(1<<bit) != 0 {
				if i >= len(in) {
					return nil, errors.New("pbo: compressed data ends early")
				}
				out = append(out, in[i])
				i++
				continue
			}
			if i+1 >= len(in) {
				return nil, errors.New("pbo: compressed data ends early")
			}
			b1, b2 := int(in[i]), int(in[i+1])
			i += 2
			dist := b1 | (b2&0xF0)<<4
			n := (b2 & 0x0F) + 3
			start := len(out) - dist
			for k := 0; k < n && len(out) < outLen; k++ {
				if p := start + k; p < 0 || p >= len(out) {
					out = append(out, ' ')
				} else {
					out = append(out, out[p])
				}
			}
		}
	}
	return out, nil
}
