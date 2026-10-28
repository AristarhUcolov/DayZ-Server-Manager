// Copyright (c) 2026 Aristarh Ucolov.
package pbo

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
)

// WriteFile is one file to pack: its path inside the archive (slashes are
// stored as backslashes, as the game's tools do) and its content.
type WriteFile struct {
	Name string
	Data []byte
}

// Write packs files into an uncompressed PBO at path: the header with the
// product entry and its properties (prefix, …), the file table, the data and
// the trailing SHA-1 of everything before it. The file is written next to
// path and renamed into place, so a reader never sees half an archive.
func Write(path string, props [][2]string, files []WriteFile) error {
	var b bytes.Buffer
	u32 := func(v uint32) { _ = binary.Write(&b, binary.LittleEndian, v) }
	z := func(s string) { b.WriteString(s); b.WriteByte(0) }
	// Product entry: empty name, "Vers" method, then the properties.
	z("")
	u32(methodProduct)
	u32(0)
	u32(0)
	u32(0)
	u32(0)
	for _, kv := range props {
		z(kv[0])
		z(kv[1])
	}
	z("")
	for _, f := range files {
		z(strings.ReplaceAll(f.Name, "/", "\\"))
		u32(methodPlain)
		u32(0) // original size: only for packed entries
		u32(0)
		u32(0)
		u32(uint32(len(f.Data)))
	}
	// End of the table: an empty entry.
	z("")
	for i := 0; i < 5; i++ {
		u32(0)
	}
	for _, f := range files {
		b.Write(f.Data)
	}
	sum := sha1.Sum(b.Bytes())
	b.WriteByte(0)
	b.Write(sum[:])

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
