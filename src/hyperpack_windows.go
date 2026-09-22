package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// HyperPack Windows standalone frontend.
// Application release: 0.3.1 Xtreme
// Experimental high-compression build: much larger dictionary, deeper candidate search, 1 MiB matches.
// Compression stream remains HPK1/v1 for compatibility with v0.1 single-file archives.

const (
	magic        uint32 = 0x314B5048 // HPK1 LE
	version             = 1
	windowSize          = 65535
	maxMatch            = 258
	minMatch            = 3
	defaultBlock        = 1 << 20

	archiveMagic   uint32 = 0x414B5048 // HPKA LE
	archiveVersion        = 1
)

type fileHeader struct {
	Magic        uint32
	Version      uint8
	Flags        uint8
	Reserved     uint16
	BlockSize    uint32
	BlockCount   uint32
	OriginalSize uint64
	OriginalCRC  uint32
}

type blockHeader struct {
	Uncompressed uint32
	Compressed   uint32
	CRC          uint32
}

type encodedBlock struct {
	data   []byte
	raw    uint32
	crc    uint32
	stored bool
}

type archiveEntry struct {
	Kind string // "file" or "dir"
	Path string // UTF-8, slash-separated
	Data []byte
}

func hash3(b []byte, p int) uint32 {
	v := uint32(b[p])<<16 | uint32(b[p+1])<<8 | uint32(b[p+2])
	v ^= v >> 7
	v *= 0x9E3779B1
	return v & 0xFFFF
}

func chainLimit(level int) int {
	// v0.2.5: substantially deeper match search. v0.2.4 was intentionally light
	// and therefore used very little CPU compared with 7-Zip/LZMA2.
	limits := [...]int{0, 2, 4, 8, 16, 24, 40, 64, 96, 128}
	if level < 0 {
		level = 0
	}
	if level > 9 {
		level = 9
	}
	return limits[level]
}

func compressBlock(prefix, block []byte, level int) encodedBlock {
	r := encodedBlock{raw: uint32(len(block)), crc: crc32.ChecksumIEEE(block)}
	if level <= 0 || len(block) == 0 {
		r.data = append([]byte(nil), block...)
		r.stored = true
		return r
	}
	input := make([]byte, 0, len(prefix)+len(block))
	input = append(input, prefix...)
	input = append(input, block...)
	base := len(prefix)
	n := len(input)
	head := make([]int32, 1<<16)
	for i := range head {
		head[i] = -1
	}
	prev := make([]int32, n)
	for i := range prev {
		prev[i] = -1
	}
	out := make([]byte, 0, len(block))
	groupPos := -1
	var control byte
	bit := 0
	tokens := 0
	begin := func() {
		groupPos = len(out)
		out = append(out, 0)
		control = 0
		bit = 0
		tokens = 0
	}
	finish := func() {
		if groupPos >= 0 {
			out[groupPos] = control
		}
	}
	insert := func(p int) {
		if p+2 >= n {
			return
		}
		h := hash3(input, p)
		prev[p] = head[h]
		head[h] = int32(p)
	}
	if base >= 3 {
		start := base - windowClamp(base)
		for p := start; p < base && p+2 < n; p++ {
			insert(p)
		}
	}
	pos := base
	begun := false
	limit := chainLimit(level)
	niceLen := [...]int{0, 8, 16, 24, 32, 48, 64, 96, 128, 192}[level]
	for pos < n {
		if !begun || tokens == 8 {
			if begun {
				finish()
			}
			begin()
			begun = true
		}
		bestLen, bestDist := 0, 0
		if pos+minMatch <= n {
			cand := head[hash3(input, pos)]
			examined := 0
			for cand >= 0 && examined < limit {
				c := int(cand)
				dist := pos - c
				if dist == 0 || dist > windowSize {
					break
				}
				maxLen := maxMatch
				if n-pos < maxLen {
					maxLen = n - pos
				}
				ln := 0
				for ln < maxLen && input[c+ln] == input[pos+ln] {
					ln++
				}
				if ln >= minMatch && ln > bestLen {
					bestLen = ln
					bestDist = dist
					if ln == maxLen {
						break
					}
				}
				if niceLen > 0 && bestLen >= niceLen {
					break
				}
				cand = prev[c]
				examined++
			}
		}
		if bestLen >= minMatch {
			ln := bestLen
			if ln > maxMatch {
				ln = maxMatch
			}
			control |= 1 << bit
			out = append(out, byte(bestDist), byte(bestDist>>8), byte(ln-minMatch))
			for p := pos; p < pos+ln; p++ {
				insert(p)
			}
			pos += ln
		} else {
			out = append(out, input[pos])
			insert(pos)
			pos++
		}
		bit++
		tokens++
	}
	if begun {
		finish()
	}
	r.data = out
	if len(r.data) >= len(block) {
		r.data = append([]byte(nil), block...)
		r.stored = true
	}
	return r
}

func windowClamp(base int) int {
	if base > windowSize {
		return windowSize
	}
	return base
}

func decodeBlock(prefix, encoded []byte, expected int, stored bool) ([]byte, error) {
	if stored {
		if len(encoded) != expected {
			return nil, fmt.Errorf("stored block size mismatch")
		}
		return append([]byte(nil), encoded...), nil
	}
	all := make([]byte, 0, len(prefix)+expected)
	all = append(all, prefix...)
	out := make([]byte, 0, expected)
	p := 0
	for len(out) < expected {
		if p >= len(encoded) {
			return nil, fmt.Errorf("truncated token stream")
		}
		control := encoded[p]
		p++
		for bit := 0; bit < 8 && len(out) < expected; bit++ {
			if (control & (1 << bit)) == 0 {
				if p >= len(encoded) {
					return nil, fmt.Errorf("truncated literal")
				}
				out = append(out, encoded[p])
				all = append(all, encoded[p])
				p++
			} else {
				if p+3 > len(encoded) {
					return nil, fmt.Errorf("truncated match")
				}
				dist := int(binary.LittleEndian.Uint16(encoded[p : p+2]))
				ln := int(encoded[p+2]) + minMatch
				p += 3
				if dist <= 0 || dist > len(all) || ln > expected-len(out) {
					return nil, fmt.Errorf("invalid match")
				}
				for i := 0; i < ln; i++ {
					src := len(all) - dist
					b := all[src]
					all = append(all, b)
					out = append(out, b)
				}
			}
		}
	}
	if p != len(encoded) {
		return nil, fmt.Errorf("trailing bytes")
	}
	return out, nil
}

func writeHeader(w io.Writer, h fileHeader) error {
	var b [28]byte
	binary.LittleEndian.PutUint32(b[0:4], h.Magic)
	b[4] = h.Version
	b[5] = h.Flags
	binary.LittleEndian.PutUint16(b[6:8], h.Reserved)
	binary.LittleEndian.PutUint32(b[8:12], h.BlockSize)
	binary.LittleEndian.PutUint32(b[12:16], h.BlockCount)
	binary.LittleEndian.PutUint64(b[16:24], h.OriginalSize)
	binary.LittleEndian.PutUint32(b[24:28], h.OriginalCRC)
	_, err := w.Write(b[:])
	return err
}

func readHeader(r io.Reader) (fileHeader, error) {
	var b [28]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return fileHeader{}, err
	}
	return fileHeader{
		Magic: binary.LittleEndian.Uint32(b[0:4]), Version: b[4], Flags: b[5],
		Reserved: binary.LittleEndian.Uint16(b[6:8]), BlockSize: binary.LittleEndian.Uint32(b[8:12]),
		BlockCount: binary.LittleEndian.Uint32(b[12:16]), OriginalSize: binary.LittleEndian.Uint64(b[16:24]),
		OriginalCRC: binary.LittleEndian.Uint32(b[24:28]),
	}, nil
}

func writeBlockHeader(w io.Writer, h blockHeader) error {
	var b [12]byte
	binary.LittleEndian.PutUint32(b[0:4], h.Uncompressed)
	binary.LittleEndian.PutUint32(b[4:8], h.Compressed)
	binary.LittleEndian.PutUint32(b[8:12], h.CRC)
	_, e := w.Write(b[:])
	return e
}

func readBlockHeader(r io.Reader) (blockHeader, error) {
	var b [12]byte
	if _, e := io.ReadFull(r, b[:]); e != nil {
		return blockHeader{}, e
	}
	return blockHeader{
		Uncompressed: binary.LittleEndian.Uint32(b[0:4]),
		Compressed:   binary.LittleEndian.Uint32(b[4:8]),
		CRC:          binary.LittleEndian.Uint32(b[8:12]),
	}, nil
}

func prefixFor(blocks [][]byte, index int) []byte {
	if index <= 0 {
		return nil
	}
	p := blocks[index-1]
	if len(p) > windowSize {
		p = p[len(p)-windowSize:]
	}
	return p
}

func compressBytes(data []byte, level int) ([]byte, error) {
	blockSize := defaultBlock
	count := (len(data) + blockSize - 1) / blockSize
	if count == 0 {
		count = 1
	}
	blocks := make([][]byte, count)
	for i := 0; i < count; i++ {
		a := i * blockSize
		b := a + blockSize
		if a > len(data) {
			a = len(data)
		}
		if b > len(data) {
			b = len(data)
		}
		blocks[i] = data[a:b]
	}

	enc := make([]encodedBlock, count)
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			enc[i] = compressBlock(prefixFor(blocks, i), blocks[i], level)
		}(i)
	}
	wg.Wait()

	var out bytes.Buffer
	h := fileHeader{Magic: magic, Version: version, Flags: 0, BlockSize: uint32(blockSize), BlockCount: uint32(count), OriginalSize: uint64(len(data)), OriginalCRC: crc32.ChecksumIEEE(data)}
	if err := writeHeader(&out, h); err != nil {
		return nil, err
	}
	for i, b := range enc {
		if err := writeBlockHeader(&out, blockHeader{uint32(len(blocks[i])), uint32(len(b.data)), b.crc}); err != nil {
			return nil, err
		}
		if _, err := out.Write(b.data); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func decompressBytes(archive []byte) ([]byte, error) {
	r := bytes.NewReader(archive)
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	if h.Magic != magic || h.Version != version {
		return nil, fmt.Errorf("not a HyperPack v0.1/v0.2 archive")
	}
	if h.BlockSize == 0 || h.BlockSize > (64<<20) {
		return nil, fmt.Errorf("invalid block size")
	}
	out := make([]byte, 0, h.OriginalSize)
	var prefix []byte
	for i := uint32(0); i < h.BlockCount; i++ {
		bh, e := readBlockHeader(r)
		if e != nil {
			return nil, e
		}
		data := make([]byte, bh.Compressed)
		if _, e = io.ReadFull(r, data); e != nil {
			return nil, e
		}
		stored := bh.Compressed == bh.Uncompressed
		dec, e := decodeBlock(prefix, data, int(bh.Uncompressed), stored)
		if e != nil {
			return nil, fmt.Errorf("block %d: %w", i, e)
		}
		if crc32.ChecksumIEEE(dec) != bh.CRC {
			return nil, fmt.Errorf("block %d CRC mismatch", i)
		}
		out = append(out, dec...)
		prefix = dec
		if len(prefix) > windowSize {
			prefix = prefix[len(prefix)-windowSize:]
		}
	}
	if uint64(len(out)) != h.OriginalSize || crc32.ChecksumIEEE(out) != h.OriginalCRC {
		return nil, fmt.Errorf("archive integrity check failed")
	}
	return out, nil
}

func compressFile(inPath, outPath string, level int) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	packed, err := compressBytes(data, level)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, packed, 0644)
}

func decompressFile(inPath, outPath string) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	raw, err := decompressBytes(data)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, raw, 0644)
}

func normalizeArchivePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	return p
}

func safeArchivePath(p string) bool {
	p = normalizeArchivePath(p)
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func collectTree(root string) ([]archiveEntry, error) {
	root = filepath.Clean(root)
	base := filepath.Base(root)
	entries := []archiveEntry{{Kind: "dir", Path: normalizeArchivePath(base)}}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		rel = normalizeArchivePath(rel)
		if !safeArchivePath(rel) {
			return fmt.Errorf("unsafe relative path: %s", rel)
		}
		if d.IsDir() {
			entries = append(entries, archiveEntry{Kind: "dir", Path: rel})
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{Kind: "file", Path: rel, Data: b})
		return nil
	})
	return entries, err
}

func commonDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	dirs := make([]string, 0, len(paths))
	for _, p := range paths {
		dirs = append(dirs, filepath.Dir(p))
	}
	base := filepath.Clean(dirs[0])
	for _, d := range dirs[1:] {
		d = filepath.Clean(d)
		for !samePathPrefix(d, base) {
			parent := filepath.Dir(base)
			if parent == base {
				return base
			}
			base = parent
		}
	}
	return base
}

func samePathPrefix(a, b string) bool {
	ar := strings.TrimRight(strings.ToLower(filepath.Clean(a)), `\`)
	br := strings.TrimRight(strings.ToLower(filepath.Clean(b)), `\`)
	if ar == br {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(ar, br+sep)
}

func collectFiles(paths []string) ([]archiveEntry, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files selected")
	}
	root := commonDir(paths)
	entries := make([]archiveEntry, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, err
		}
		rel = normalizeArchivePath(rel)
		if !safeArchivePath(rel) {
			return nil, fmt.Errorf("unsafe relative path: %s", rel)
		}
		if seen[strings.ToLower(rel)] {
			return nil, fmt.Errorf("duplicate file name: %s", rel)
		}
		seen[strings.ToLower(rel)] = true
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		entries = append(entries, archiveEntry{Kind: "file", Path: rel, Data: b})
	}
	return entries, nil
}

func encodePackage(entries []archiveEntry) ([]byte, error) {
	var out bytes.Buffer
	var hdr [12]byte
	binary.LittleEndian.PutUint32(hdr[0:4], archiveMagic)
	binary.LittleEndian.PutUint16(hdr[4:6], archiveVersion)
	binary.LittleEndian.PutUint16(hdr[6:8], 0)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(len(entries)))
	out.Write(hdr[:])
	for _, e := range entries {
		p := normalizeArchivePath(e.Path)
		if !safeArchivePath(p) {
			return nil, fmt.Errorf("unsafe archive path: %s", p)
		}
		pb := []byte(p)
		if len(pb) > 1<<20 {
			return nil, fmt.Errorf("path too long")
		}
		kind := byte(0)
		if e.Kind == "dir" {
			kind = 1
		}
		var eh [17]byte
		eh[0] = kind
		binary.LittleEndian.PutUint32(eh[1:5], uint32(len(pb)))
		binary.LittleEndian.PutUint64(eh[5:13], uint64(len(e.Data)))
		binary.LittleEndian.PutUint32(eh[13:17], crc32.ChecksumIEEE(e.Data))
		out.Write(eh[:])
		out.Write(pb)
		if len(e.Data) > 0 {
			out.Write(e.Data)
		}
	}
	return out.Bytes(), nil
}

func decodePackage(data []byte) ([]archiveEntry, bool, error) {
	if len(data) < 12 || binary.LittleEndian.Uint32(data[0:4]) != archiveMagic {
		return nil, false, nil
	}
	if binary.LittleEndian.Uint16(data[4:6]) != archiveVersion {
		return nil, true, fmt.Errorf("unsupported HyperPack package version")
	}
	count := binary.LittleEndian.Uint32(data[8:12])
	if count > 1_000_000 {
		return nil, true, fmt.Errorf("invalid entry count")
	}
	pos := 12
	entries := make([]archiveEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		if pos+17 > len(data) {
			return nil, true, fmt.Errorf("truncated package entry")
		}
		kind := data[pos]
		pathLen := int(binary.LittleEndian.Uint32(data[pos+1 : pos+5]))
		size := binary.LittleEndian.Uint64(data[pos+5 : pos+13])
		wantCRC := binary.LittleEndian.Uint32(data[pos+13 : pos+17])
		pos += 17
		if pathLen < 0 || pos+pathLen > len(data) {
			return nil, true, fmt.Errorf("invalid package path")
		}
		path := string(data[pos : pos+pathLen])
		pos += pathLen
		if !safeArchivePath(path) {
			return nil, true, fmt.Errorf("unsafe package path: %s", path)
		}
		if size > uint64(len(data)-pos) {
			return nil, true, fmt.Errorf("invalid package entry size")
		}
		b := append([]byte(nil), data[pos:pos+int(size)]...)
		pos += int(size)
		if crc32.ChecksumIEEE(b) != wantCRC {
			return nil, true, fmt.Errorf("CRC mismatch: %s", path)
		}
		if kind != 0 && kind != 1 {
			return nil, true, fmt.Errorf("invalid package entry type")
		}
		entries = append(entries, archiveEntry{Kind: map[bool]string{true: "dir", false: "file"}[kind == 1], Path: path, Data: b})
	}
	if pos != len(data) {
		return nil, true, fmt.Errorf("trailing bytes in package")
	}
	return entries, true, nil
}

func createPackageArchive(entries []archiveEntry, outPath string, level int) error {
	pkg, err := encodePackage(entries)
	if err != nil {
		return err
	}
	packed, err := compressBytes(pkg, level)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, packed, 0644)
}

func extractPackage(entries []archiveEntry, root string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		if !safeArchivePath(e.Path) {
			return fmt.Errorf("unsafe path: %s", e.Path)
		}
		target := filepath.Join(root, filepath.FromSlash(e.Path))
		if e.Kind == "dir" {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, e.Data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------- Streaming / bounded-memory I/O ---------------------
// v0.2.4: the previous GUI path loaded large files multiple times into RAM:
// source file -> package buffer -> compressed buffer -> output. A large
// StarCraft/MPQ-style file could therefore consume several times its size and
// get terminated by Windows under memory pressure. The release path below is
// streaming and keeps only one block (+ a small dictionary prefix) in RAM.

type archiveSource struct {
	Kind       string // file or dir
	Path       string // archive-relative path
	SourcePath string // source filesystem path for files
}

type progressFunc func(done, total int64, stage string)

func crcAndSizeFile(path string) (uint32, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	buf := make([]byte, 1<<20)
	var total int64
	for {
		n, er := f.Read(buf)
		if n > 0 {
			if _, err := h.Write(buf[:n]); err != nil {
				return 0, 0, err
			}
			total += int64(n)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return 0, 0, er
		}
	}
	return h.Sum32(), total, nil
}

func collectFileSources(paths []string) ([]archiveSource, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files selected")
	}
	root := commonDir(paths)
	entries := make([]archiveSource, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, err
		}
		rel = normalizeArchivePath(rel)
		if !safeArchivePath(rel) {
			return nil, fmt.Errorf("unsafe relative path: %s", rel)
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return nil, fmt.Errorf("duplicate file name: %s", rel)
		}
		seen[key] = true
		entries = append(entries, archiveSource{Kind: "file", Path: rel, SourcePath: p})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no regular files selected")
	}
	return entries, nil
}

func collectTreeSources(root string) ([]archiveSource, error) {
	root = filepath.Clean(root)
	base := filepath.Base(root)
	entries := []archiveSource{{Kind: "dir", Path: normalizeArchivePath(base)}}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		rel = normalizeArchivePath(rel)
		if !safeArchivePath(rel) {
			return fmt.Errorf("unsafe relative path: %s", rel)
		}
		if d.IsDir() {
			entries = append(entries, archiveSource{Kind: "dir", Path: rel})
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		entries = append(entries, archiveSource{Kind: "file", Path: rel, SourcePath: path})
		return nil
	})
	return entries, err
}

func writePackageTemp(entries []archiveSource, tempDir string, progress progressFunc) (string, error) {
	if uint64(len(entries)) > uint64(^uint32(0)) {
		return "", fmt.Errorf("too many package entries")
	}
	f, err := os.CreateTemp(tempDir, ".hyperpack-package-*.tmp")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()

	var hdr [12]byte
	binary.LittleEndian.PutUint32(hdr[0:4], archiveMagic)
	binary.LittleEndian.PutUint16(hdr[4:6], archiveVersion)
	binary.LittleEndian.PutUint16(hdr[6:8], 0)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(len(entries)))
	if _, err := f.Write(hdr[:]); err != nil {
		return "", err
	}

	for i, e := range entries {
		p := normalizeArchivePath(e.Path)
		if !safeArchivePath(p) {
			return "", fmt.Errorf("unsafe archive path: %s", p)
		}
		pb := []byte(p)
		if len(pb) > 1<<20 {
			return "", fmt.Errorf("path too long")
		}
		kind := byte(0)
		if e.Kind == "dir" {
			kind = 1
		}
		var size int64
		var crc uint32
		if kind == 0 {
			crc, size, err = crcAndSizeFile(e.SourcePath)
			if err != nil {
				return "", err
			}
		}
		if size < 0 {
			return "", fmt.Errorf("invalid file size")
		}
		var eh [17]byte
		eh[0] = kind
		binary.LittleEndian.PutUint32(eh[1:5], uint32(len(pb)))
		binary.LittleEndian.PutUint64(eh[5:13], uint64(size))
		binary.LittleEndian.PutUint32(eh[13:17], crc)
		if _, err := f.Write(eh[:]); err != nil {
			return "", err
		}
		if _, err := f.Write(pb); err != nil {
			return "", err
		}
		if kind == 0 {
			in, er := os.Open(e.SourcePath)
			if er != nil {
				return "", er
			}
			written, er := io.Copy(f, in)
			_ = in.Close()
			if er != nil {
				return "", er
			}
			if written != size {
				return "", fmt.Errorf("file changed while packaging: %s", e.SourcePath)
			}
		}
		if progress != nil {
			progress(int64(i+1), int64(len(entries)), "패키지 준비")
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func workerCount() int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	return workers
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func compressionBlockSize(level int) int64 {
	// Smaller blocks at higher levels create enough independent work to keep
	// many logical CPUs busy while the 64 KiB history window still preserves
	// long-distance matches between adjacent blocks.
	if level >= 8 {
		return 256 << 10 // 256 KiB
	}
	return 512 << 10 // 512 KiB
}

type compressJob struct {
	index  int
	block  []byte
	prefix []byte
}

type compressResult struct {
	index int
	enc   encodedBlock
	err   error
}

// ---------------------------- HPK4 high-compression core -------------------
// v0.3.2: Xtreme+ engine.
//
// Design goals:
//   * Much larger LZ window than the demo HPK1/2/3 engines.
//   * Solid-stream friendly and bounded-memory streaming I/O.
//   * Multiple worker processes/jobs so a large archive can use many CPU cores.
//   * A second entropy stage (DEFLATE) is applied to the custom token stream
//     only when it actually makes the block smaller.
//   * Optional OpenCL GPU hash precomputation is used when a GPU OpenCL runtime
//     is available. It is an assist stage, never a hard dependency.
//
// The format is intentionally new (HPK4) so the older experimental streams
// remain readable by their historical readers. The GUI writes HPK4.

const (
	hpk4Magic       uint32 = 0x344B5048 // "HPK4" LE
	hpk4Version     byte   = 1
	hpk4FlagPackage byte   = 1

	hpk4MinMatch = 4
	hpk4MaxMatch = 65535
	hpk4HashBits = 19
	hpk4HashSize = 1 << hpk4HashBits
	hpk4Slots    = 32

	hpk4ModeLZ    byte = 0
	hpk4ModeRaw   byte = 1
	hpk4ModeFlate byte = 2
)

type hpk4Header struct {
	Magic    uint32
	Flags    byte
	Group    uint32
	Dict     uint32
	Groups   uint32
	Entries  uint32
	Manifest uint64
	Original uint64
	CRC      uint32
}

func hpk4WriteHeader(w io.Writer, h hpk4Header) error {
	var b [44]byte
	binary.LittleEndian.PutUint32(b[0:4], h.Magic)
	b[4] = hpk4Version
	b[5] = h.Flags
	binary.LittleEndian.PutUint32(b[8:12], h.Group)
	binary.LittleEndian.PutUint32(b[12:16], h.Dict)
	binary.LittleEndian.PutUint32(b[16:20], h.Groups)
	binary.LittleEndian.PutUint32(b[20:24], h.Entries)
	binary.LittleEndian.PutUint64(b[24:32], h.Manifest)
	binary.LittleEndian.PutUint64(b[32:40], h.Original)
	binary.LittleEndian.PutUint32(b[40:44], h.CRC)
	_, err := w.Write(b[:])
	return err
}

func hpk4ReadHeader(r io.Reader) (hpk4Header, error) {
	var b [44]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return hpk4Header{}, err
	}
	if binary.LittleEndian.Uint32(b[0:4]) != hpk4Magic || b[4] != hpk4Version {
		return hpk4Header{}, fmt.Errorf("not a HyperPack HPK4 archive")
	}
	return hpk4Header{
		Magic: binary.LittleEndian.Uint32(b[0:4]), Flags: b[5],
		Group: binary.LittleEndian.Uint32(b[8:12]), Dict: binary.LittleEndian.Uint32(b[12:16]),
		Groups: binary.LittleEndian.Uint32(b[16:20]), Entries: binary.LittleEndian.Uint32(b[20:24]),
		Manifest: binary.LittleEndian.Uint64(b[24:32]), Original: binary.LittleEndian.Uint64(b[32:40]),
		CRC: binary.LittleEndian.Uint32(b[40:44]),
	}, nil
}

func hpk4Options(level int) (groupSize, dictSize int64) {
	switch {
	case level >= 9:
		// Xtreme+: large dictionary + enough independent groups to feed many
		// workers. 64 MiB keeps the memory bounded on ordinary desktops.
		return 8 << 20, 128 << 20
	case level >= 8:
		return 8 << 20, 96 << 20
	case level >= 7:
		return 4 << 20, 32 << 20
	case level >= 5:
		return 2 << 20, 16 << 20
	default:
		return 1 << 20, 8 << 20
	}
}

func hpk4MaxCandidates(level int) int {
	switch {
	case level >= 9:
		return hpk4Slots
	case level >= 8:
		return 20
	case level >= 7:
		return 16
	case level >= 5:
		return 12
	default:
		return 8
	}
}

func hpk4Hash8(b []byte, p int) uint32 {
	var v uint64
	v |= uint64(b[p+0]) << 56
	v |= uint64(b[p+1]) << 48
	v |= uint64(b[p+2]) << 40
	v |= uint64(b[p+3]) << 32
	v |= uint64(b[p+4]) << 24
	v |= uint64(b[p+5]) << 16
	v |= uint64(b[p+6]) << 8
	v |= uint64(b[p+7])
	v ^= v >> 33
	v *= 0xff51afd7ed558ccd
	v ^= v >> 33
	v *= 0xc4ceb9fe1a85ec53
	v ^= v >> 33
	return uint32(v) & (hpk4HashSize - 1)
}

func hpk4PutVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func hpk4GetVarint(data []byte, pos *int) (uint64, error) {
	var v uint64
	var shift uint
	for *pos < len(data) && shift <= 63 {
		b := data[*pos]
		*pos++
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v, nil
		}
		shift += 7
	}
	return 0, fmt.Errorf("truncated varint")
}

// OpenCL GPU hash assist. This is deliberately isolated from the compressor:
// missing OpenCL, driver problems, or a build failure simply disable the assist.
var hpk4GPUMutex sync.Mutex

var (
	clOnce           sync.Once
	clAvailable      bool
	clGetPlatformIDs *syscall.LazyProc
	clGetDeviceIDs   *syscall.LazyProc
	clCreateContext  *syscall.LazyProc
	clCreateQueue    *syscall.LazyProc
	clCreateProgram  *syscall.LazyProc
	clBuildProgram   *syscall.LazyProc
	clCreateKernel   *syscall.LazyProc
	clCreateBuffer   *syscall.LazyProc
	clSetKernelArg   *syscall.LazyProc
	clWriteBuffer    *syscall.LazyProc
	clNDRange        *syscall.LazyProc
	clFinish         *syscall.LazyProc
	clReadBuffer     *syscall.LazyProc
	clReleaseKernel  *syscall.LazyProc
	clReleaseProgram *syscall.LazyProc
	clReleaseQueue   *syscall.LazyProc
	clReleaseContext *syscall.LazyProc
	clReleaseMem     *syscall.LazyProc
	clDeviceTypeGPU  = uintptr(1 << 2)
	clMemReadOnly    = uintptr(1 << 2)
	clMemWriteOnly   = uintptr(1 << 1)
)

const hpk4OpenCLKernels = `
__kernel void hp_hash8(__global const uchar* data,
                       __global uint* out,
                       uint n) {
    uint i = get_global_id(0);
    if (i + 8 > n) { return; }
    ulong v = 0;
    v |= ((ulong)data[i+0]) << 56;
    v |= ((ulong)data[i+1]) << 48;
    v |= ((ulong)data[i+2]) << 40;
    v |= ((ulong)data[i+3]) << 32;
    v |= ((ulong)data[i+4]) << 24;
    v |= ((ulong)data[i+5]) << 16;
    v |= ((ulong)data[i+6]) << 8;
    v |= ((ulong)data[i+7]);
    v ^= v >> 33;
    v *= (ulong)0xff51afd7ed558ccdUL;
    v ^= v >> 33;
    v *= (ulong)0xc4ceb9fe1a85ec53UL;
    v ^= v >> 33;
    out[i] = (uint)(v & 0x7ffffUL);
}
`

func initOpenCL() {
	clOnce.Do(func() {
		dll := syscall.NewLazyDLL("OpenCL.dll")
		if err := dll.Load(); err != nil {
			return
		}
		clGetPlatformIDs = dll.NewProc("clGetPlatformIDs")
		clGetDeviceIDs = dll.NewProc("clGetDeviceIDs")
		clCreateContext = dll.NewProc("clCreateContext")
		clCreateQueue = dll.NewProc("clCreateCommandQueue")
		clCreateProgram = dll.NewProc("clCreateProgramWithSource")
		clBuildProgram = dll.NewProc("clBuildProgram")
		clCreateKernel = dll.NewProc("clCreateKernel")
		clCreateBuffer = dll.NewProc("clCreateBuffer")
		clSetKernelArg = dll.NewProc("clSetKernelArg")
		clWriteBuffer = dll.NewProc("clEnqueueWriteBuffer")
		clNDRange = dll.NewProc("clEnqueueNDRangeKernel")
		clFinish = dll.NewProc("clFinish")
		clReadBuffer = dll.NewProc("clEnqueueReadBuffer")
		clReleaseKernel = dll.NewProc("clReleaseKernel")
		clReleaseProgram = dll.NewProc("clReleaseProgram")
		clReleaseQueue = dll.NewProc("clReleaseCommandQueue")
		clReleaseContext = dll.NewProc("clReleaseContext")
		clReleaseMem = dll.NewProc("clReleaseMemObject")
		if clGetPlatformIDs.Find() != nil || clGetDeviceIDs.Find() != nil ||
			clCreateContext.Find() != nil || clCreateQueue.Find() != nil ||
			clCreateProgram.Find() != nil || clBuildProgram.Find() != nil ||
			clCreateKernel.Find() != nil || clCreateBuffer.Find() != nil ||
			clSetKernelArg.Find() != nil || clWriteBuffer.Find() != nil ||
			clNDRange.Find() != nil || clFinish.Find() != nil ||
			clReadBuffer.Find() != nil {
			return
		}
		var nPlatforms uint32
		r, _, _ := clGetPlatformIDs.Call(0, 0, uintptr(unsafe.Pointer(&nPlatforms)))
		clAvailable = r == 0 && nPlatforms > 0
	})
}

func hpk4GPUHash8(data []byte) (result []uint32, ok bool) {
	if os.Getenv("HYPERPACK_NO_GPU") == "1" {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			result, ok = nil, false
		}
	}()
	hpk4GPUMutex.Lock()
	defer hpk4GPUMutex.Unlock()
	if len(data) < 8 {
		return nil, false
	}
	initOpenCL()
	if !clAvailable {
		return nil, false
	}
	// Avoid sending tiny groups to the GPU; PCIe/driver overhead dominates.
	if len(data) < 8<<20 {
		return nil, false
	}
	var nPlatforms uint32
	platforms := make([]uintptr, 8)
	if r, _, _ := clGetPlatformIDs.Call(8, uintptr(unsafe.Pointer(&platforms[0])), uintptr(unsafe.Pointer(&nPlatforms))); r != 0 || nPlatforms == 0 {
		return nil, false
	}
	var device uintptr
	var nDevices uint32
	for i := uint32(0); i < nPlatforms; i++ {
		if r, _, _ := clGetDeviceIDs.Call(platforms[i], clDeviceTypeGPU, 1, uintptr(unsafe.Pointer(&device)), uintptr(unsafe.Pointer(&nDevices))); r == 0 && nDevices > 0 {
			break
		}
		device = 0
	}
	if device == 0 {
		return nil, false
	}
	var errCode int32
	context, _, _ := clCreateContext.Call(0, 1, uintptr(unsafe.Pointer(&device)), 0, 0, uintptr(unsafe.Pointer(&errCode)))
	if context == 0 || errCode != 0 {
		return nil, false
	}
	defer clReleaseContext.Call(context)
	queue, _, _ := clCreateQueue.Call(context, device, 0, uintptr(unsafe.Pointer(&errCode)))
	if queue == 0 || errCode != 0 {
		return nil, false
	}
	defer clReleaseQueue.Call(queue)
	srcPtr := uintptr(unsafe.Pointer(&[]byte(hpk4OpenCLKernels)[0]))
	cstr := syscall.StringBytePtr(hpk4OpenCLKernels)
	sources := []uintptr{uintptr(unsafe.Pointer(&cstr))}
	var lengths []uintptr
	_ = srcPtr
	lengths = []uintptr{uintptr(len(hpk4OpenCLKernels))}
	program, _, _ := clCreateProgram.Call(context, 1, uintptr(unsafe.Pointer(&sources[0])), uintptr(unsafe.Pointer(&lengths[0])), uintptr(unsafe.Pointer(&errCode)))
	if program == 0 || errCode != 0 {
		return nil, false
	}
	defer clReleaseProgram.Call(program)
	if r, _, _ := clBuildProgram.Call(program, 1, uintptr(unsafe.Pointer(&device)), 0, 0, 0); r != 0 {
		return nil, false
	}
	kernelName := syscall.StringBytePtr("hp_hash8")
	kernel, _, _ := clCreateKernel.Call(program, uintptr(unsafe.Pointer(kernelName)), uintptr(unsafe.Pointer(&errCode)))
	if kernel == 0 || errCode != 0 {
		return nil, false
	}
	defer clReleaseKernel.Call(kernel)
	inBytes := uintptr(len(data))
	outCount := len(data)
	inBuf, _, _ := clCreateBuffer.Call(context, clMemReadOnly, inBytes, 0, uintptr(unsafe.Pointer(&errCode)))
	if inBuf == 0 || errCode != 0 {
		return nil, false
	}
	defer clReleaseMem.Call(inBuf)
	outBuf, _, _ := clCreateBuffer.Call(context, clMemWriteOnly, uintptr(outCount*4), 0, uintptr(unsafe.Pointer(&errCode)))
	if outBuf == 0 || errCode != 0 {
		return nil, false
	}
	defer clReleaseMem.Call(outBuf)
	if r, _, _ := clWriteBuffer.Call(queue, inBuf, 1, 0, inBytes, uintptr(unsafe.Pointer(&data[0])), 0, 0, 0); r != 0 {
		return nil, false
	}
	args := []uintptr{inBuf, outBuf, uintptr(len(data))}
	for i, a := range args {
		var sz uintptr = unsafe.Sizeof(a)
		if i == 2 {
			v := uint32(len(data))
			sz = 4
			if r, _, _ := clSetKernelArg.Call(kernel, uintptr(i), sz, uintptr(unsafe.Pointer(&v))); r != 0 {
				return nil, false
			}
		} else if r, _, _ := clSetKernelArg.Call(kernel, uintptr(i), sz, uintptr(unsafe.Pointer(&a))); r != 0 {
			return nil, false
		}
	}
	global := uintptr(len(data) - 7)
	if r, _, _ := clNDRange.Call(queue, kernel, 1, 0, uintptr(unsafe.Pointer(&global)), 0, 0, 0, 0); r != 0 {
		return nil, false
	}
	if r, _, _ := clFinish.Call(queue); r != 0 {
		return nil, false
	}
	out := make([]uint32, outCount)
	if r, _, _ := clReadBuffer.Call(queue, outBuf, 1, 0, uintptr(outCount*4), uintptr(unsafe.Pointer(&out[0])), 0, 0, 0); r != 0 {
		return nil, false
	}
	return out, true
}

func hpk4Squeeze(input []byte) ([]byte, bool) {
	if len(input) < 1024 {
		return input, false
	}
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.BestCompression)
	if err != nil {
		return input, false
	}
	if _, err := w.Write(input); err != nil {
		_ = w.Close()
		return input, false
	}
	if err := w.Close(); err != nil {
		return input, false
	}
	if b.Len()+8 >= len(input) {
		return input, false
	}
	return b.Bytes(), true
}

func hpk4Unsqueeze(input []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(input))
	defer r.Close()
	return io.ReadAll(r)
}

func hpk4EncodeGroup(raw, prefix []byte, level int, dict uint32) (encoded []byte, mode byte, gpuUsed bool, err error) {
	if len(raw) == 0 {
		return nil, hpk4ModeRaw, false, nil
	}
	data := make([]byte, len(prefix)+len(raw))
	copy(data, prefix)
	copy(data[len(prefix):], raw)
	base := len(prefix)
	n := len(data)

	// Hash-ring index. 24 slots per bucket gives a deep candidate pool while
	// keeping memory predictable enough to run several worker jobs at once.
	slots := make([]uint32, hpk4HashSize*hpk4Slots)
	for i := range slots {
		slots[i] = ^uint32(0)
	}
	cursors := make([]uint8, hpk4HashSize)
	insertWithHash := func(p int, h uint32) {
		if p < 0 || p+7 >= n {
			return
		}
		b := int(h)
		idx := b*hpk4Slots + int(cursors[b])
		slots[idx] = uint32(p)
		cursors[b] = uint8((int(cursors[b]) + 1) % hpk4Slots)
	}

	hashes, gpu := hpk4GPUHash8(data)
	gpuUsed = gpu
	hashAt := func(p int) uint32 {
		if hashes != nil && p < len(hashes) {
			return hashes[p]
		}
		return hpk4Hash8(data, p)
	}
	for p := 0; p+7 < base; p++ {
		insertWithHash(p, hashAt(p))
	}

	candidates := hpk4MaxCandidates(level)
	maxDist := int(dict)
	if maxDist <= 0 {
		maxDist = 1
	}
	maxLen := hpk4MaxMatch
	out := make([]byte, 0, len(raw)/2)
	var controlPos int
	var control byte
	bit := 0
	begin := func() {
		controlPos = len(out)
		out = append(out, 0)
		control = 0
		bit = 0
	}
	finish := func() { out[controlPos] = control }
	begin()

	find := func(pos int) (int, int) {
		if pos+7 >= n {
			return 0, 0
		}
		bucket := int(hashAt(pos))
		bestLen, bestDist := 0, 0
		maxL := maxLen
		if n-pos < maxL {
			maxL = n - pos
		}
		seen := 0
		for s := 0; s < hpk4Slots && seen < candidates; s++ {
			cand := slots[bucket*hpk4Slots+s]
			if cand == ^uint32(0) {
				continue
			}
			c := int(cand)
			if c >= pos {
				continue
			}
			dist := pos - c
			if dist <= 0 || dist > maxDist {
				continue
			}
			seen++
			ln := 0
			for ln < maxL && data[c+ln] == data[pos+ln] {
				ln++
			}
			if ln >= hpk4MinMatch && ln > bestLen {
				bestLen, bestDist = ln, dist
				if ln == maxL {
					break
				}
			}
		}
		return bestLen, bestDist
	}

	pos := base
	for pos < n {
		ln, dist := find(pos)
		// 1-byte lookahead at high levels. It is cheap compared with the long
		// match scan and avoids stealing a better match one byte later.
		if level >= 8 && ln >= hpk4MinMatch && pos+1 < n {
			insertWithHash(pos, hashAt(pos))
			nl, _ := find(pos + 1)
			if nl > ln+2 {
				ln, dist = 0, 0
			}
		}
		if ln >= hpk4MinMatch {
			control |= 1 << bit
			out = hpk4PutVarint(out, uint64(dist))
			out = hpk4PutVarint(out, uint64(ln-hpk4MinMatch))
			for q := 0; q < ln; q++ {
				p := pos + q
				if p+7 < n {
					insertWithHash(p, hashAt(p))
				}
			}
			pos += ln
		} else {
			out = append(out, data[pos])
			if pos+7 < n {
				insertWithHash(pos, hashAt(pos))
			}
			pos++
		}
		bit++
		if bit == 8 && pos < n {
			finish()
			begin()
		}
	}
	finish()
	if len(out) >= len(raw) {
		return append([]byte(nil), raw...), hpk4ModeRaw, gpuUsed, nil
	}
	squeezed, ok := hpk4Squeeze(out)
	if ok {
		return squeezed, hpk4ModeFlate, gpuUsed, nil
	}
	return out, hpk4ModeLZ, gpuUsed, nil
}

func hpk4UpdatePrefix(prefix, raw []byte, dict int) []byte {
	if dict <= 0 {
		return nil
	}
	total := len(prefix) + len(raw)
	take := dict
	if total < take {
		take = total
	}
	out := make([]byte, take)
	fromRaw := len(raw)
	if fromRaw > take {
		fromRaw = take
	}
	copy(out[take-fromRaw:], raw[len(raw)-fromRaw:])
	rest := take - fromRaw
	if rest > 0 && len(prefix) > 0 {
		copy(out[:rest], prefix[len(prefix)-rest:])
	}
	return out
}

func hpk4DecodeGroup(encoded []byte, expected int, prefix []byte, dict uint32, mode byte) ([]byte, []byte, error) {
	if mode == hpk4ModeRaw {
		if len(encoded) != expected {
			return nil, prefix, fmt.Errorf("raw group size mismatch")
		}
		out := append([]byte(nil), encoded...)
		return out, hpk4UpdatePrefix(prefix, out, int(dict)), nil
	}
	if mode == hpk4ModeFlate {
		dec, err := hpk4Unsqueeze(encoded)
		if err != nil {
			return nil, prefix, err
		}
		encoded = dec
	} else if mode != hpk4ModeLZ {
		return nil, prefix, fmt.Errorf("unknown group mode")
	}
	out := make([]byte, 0, expected)
	p := 0
	for len(out) < expected {
		if p >= len(encoded) {
			return nil, prefix, fmt.Errorf("truncated control byte")
		}
		control := encoded[p]
		p++
		for bit := 0; bit < 8 && len(out) < expected; bit++ {
			if control&(1<<bit) == 0 {
				if p >= len(encoded) {
					return nil, prefix, fmt.Errorf("truncated literal")
				}
				out = append(out, encoded[p])
				p++
				continue
			}
			dist, err := hpk4GetVarint(encoded, &p)
			if err != nil {
				return nil, prefix, err
			}
			lm1, err := hpk4GetVarint(encoded, &p)
			if err != nil {
				return nil, prefix, err
			}
			ln := lm1 + hpk4MinMatch
			if dist == 0 || dist > uint64(dict) || dist > uint64(len(prefix)+len(out)) || ln > uint64(expected-len(out)) {
				return nil, prefix, fmt.Errorf("invalid match")
			}
			for i := uint64(0); i < ln; i++ {
				src := len(prefix) + len(out) - int(dist)
				if src < len(prefix) {
					out = append(out, prefix[src])
				} else {
					out = append(out, out[src-len(prefix)])
				}
			}
		}
	}
	if p != len(encoded) {
		return nil, prefix, fmt.Errorf("trailing encoded bytes")
	}
	return out, hpk4UpdatePrefix(prefix, out, int(dict)), nil
}

func hpk4WorkerCount(groups, dictSize, groupSize int64) int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	// A group needs approximately dict + group + hash-ring memory.  The cap
	// prevents a machine with 32+ logical CPUs from allocating an absurd
	// number of simultaneous 64 MiB dictionaries.
	capWorkers := 8
	if workers > capWorkers {
		workers = capWorkers
	}
	approx := dictSize + groupSize + int64(40<<20)
	if approx > 0 {
		maxByMem := int((4 << 30) / approx)
		if maxByMem < 1 {
			maxByMem = 1
		}
		if workers > maxByMem {
			workers = maxByMem
		}
	}
	if groups > 0 && int64(workers) > groups {
		workers = int(groups)
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

type hpk4Job struct {
	Index  int
	Offset int64
	Raw    int
	Prefix int
}
type hpk4Result struct {
	Index int
	Raw   int
	Enc   []byte
	Mode  byte
	CRC   uint32
	GPU   bool
	Err   error
}

func compressFileStreaming(inPath, outPath string, level int, progress progressFunc) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	total := st.Size()
	groupSize, dictSize := hpk4Options(level)
	groups := int64(0)
	if total > 0 {
		groups = (total + groupSize - 1) / groupSize
	}
	if groups > int64(^uint32(0)) {
		return fmt.Errorf("input has too many groups")
	}
	inputCRC, _, err := crcAndSizeFilePath(inPath)
	if err != nil {
		return err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = out.Close()
		if cleanup {
			_ = os.Remove(outPath)
		}
	}()
	if err := hpk4WriteHeader(out, hpk4Header{Magic: hpk4Magic, Flags: 0, Group: uint32(groupSize), Dict: uint32(dictSize), Groups: uint32(groups), Original: uint64(total), CRC: inputCRC}); err != nil {
		return err
	}
	if groups == 0 {
		cleanup = false
		return nil
	}
	workers := hpk4WorkerCount(groups, dictSize, groupSize)
	jobs := make(chan hpk4Job, workers*2)
	results := make(chan hpk4Result, workers*2)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				buf := make([]byte, job.Prefix+job.Raw)
				if _, er := in.ReadAt(buf, job.Offset-int64(job.Prefix)); er != nil && er != io.EOF {
					results <- hpk4Result{Index: job.Index, Err: er}
					continue
				}
				enc, mode, gpu, er := hpk4EncodeGroup(buf[job.Prefix:], buf[:job.Prefix], level, uint32(dictSize))
				if er != nil {
					results <- hpk4Result{Index: job.Index, Err: er}
					continue
				}
				results <- hpk4Result{Index: job.Index, Raw: job.Raw, Enc: enc, Mode: mode, CRC: crc32.ChecksumIEEE(buf[job.Prefix:]), GPU: gpu}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := int64(0); i < groups; i++ {
			off := i * groupSize
			raw := groupSize
			if r := total - off; r < raw {
				raw = r
			}
			prefix := off
			if prefix > dictSize {
				prefix = dictSize
			}
			jobs <- hpk4Job{Index: int(i), Offset: off, Raw: int(raw), Prefix: int(prefix)}
		}
	}()
	pending := make(map[int]hpk4Result, workers*2)
	next := 0
	received := 0
	gpuSeen := false
	for received < int(groups) {
		res := <-results
		received++
		if res.Err != nil {
			return res.Err
		}
		pending[res.Index] = res
		for {
			cur, ok := pending[next]
			if !ok {
				break
			}
			var hdr []byte
			hdr = hpk4PutVarint(hdr, uint64(cur.Raw))
			hdr = hpk4PutVarint(hdr, uint64(len(cur.Enc)))
			if _, er := out.Write(hdr); er != nil {
				return er
			}
			if _, er := out.Write([]byte{cur.Mode}); er != nil {
				return er
			}
			var cb [4]byte
			binary.LittleEndian.PutUint32(cb[:], cur.CRC)
			if _, er := out.Write(cb[:]); er != nil {
				return er
			}
			if _, er := out.Write(cur.Enc); er != nil {
				return er
			}
			gpuSeen = gpuSeen || cur.GPU
			delete(pending, next)
			done := minInt64(int64(next+1)*groupSize, total)
			if progress != nil {
				stage := fmt.Sprintf("Xtreme+ · CPU %d", workers)
				if gpuSeen {
					stage += " + GPU"
				}
				progress(done, total, stage)
			}
			next++
		}
	}
	wg.Wait()
	if progress != nil {
		stage := fmt.Sprintf("압축 완료 · CPU %d", workers)
		if gpuSeen {
			stage += " + GPU"
		}
		progress(total, total, stage)
	}
	cleanup = false
	return nil
}

func hpk4GetVarintFromReader(r io.Reader) (uint64, error) {
	var v uint64
	var shift uint
	var b [1]byte
	for shift <= 63 {
		if _, e := io.ReadFull(r, b[:]); e != nil {
			return 0, e
		}
		v |= uint64(b[0]&0x7f) << shift
		if b[0]&0x80 == 0 {
			return v, nil
		}
		shift += 7
	}
	return 0, fmt.Errorf("invalid varint")
}

func decompressHpkStreaming(inPath string, out io.Writer, progress progressFunc) (hpk4Header, error) {
	in, err := os.Open(inPath)
	if err != nil {
		return hpk4Header{}, err
	}
	defer in.Close()
	h, err := hpk4ReadHeader(in)
	if err != nil {
		return hpk4Header{}, err
	}
	if h.Group == 0 || h.Dict == 0 || h.Groups > (1<<28) {
		return hpk4Header{}, fmt.Errorf("invalid HPK4 header")
	}
	crc := crc32.NewIEEE()
	prefix := []byte(nil)
	var done int64
	for i := uint32(0); i < h.Groups; i++ {
		unc, er := hpk4GetVarintFromReader(in)
		if er != nil {
			return hpk4Header{}, er
		}
		comp, er := hpk4GetVarintFromReader(in)
		if er != nil {
			return hpk4Header{}, er
		}
		if unc > uint64(h.Group) || comp > (1<<30) {
			return hpk4Header{}, fmt.Errorf("invalid group size")
		}
		var mode [1]byte
		if _, er := io.ReadFull(in, mode[:]); er != nil {
			return hpk4Header{}, er
		}
		var cb [4]byte
		if _, er := io.ReadFull(in, cb[:]); er != nil {
			return hpk4Header{}, er
		}
		want := binary.LittleEndian.Uint32(cb[:])
		enc := make([]byte, int(comp))
		if _, er := io.ReadFull(in, enc); er != nil {
			return hpk4Header{}, er
		}
		dec, newPrefix, er := hpk4DecodeGroup(enc, int(unc), prefix, h.Dict, mode[0])
		if er != nil {
			return hpk4Header{}, fmt.Errorf("group %d: %w", i, er)
		}
		if crc32.ChecksumIEEE(dec) != want {
			return hpk4Header{}, fmt.Errorf("group %d CRC mismatch", i)
		}
		if _, er := crc.Write(dec); er != nil {
			return hpk4Header{}, er
		}
		if _, er := out.Write(dec); er != nil {
			return hpk4Header{}, er
		}
		prefix = newPrefix
		done += int64(len(dec))
		if progress != nil {
			progress(done, int64(h.Original), "압축 해제")
		}
	}
	if uint64(done) != h.Original || crc.Sum32() != h.CRC {
		return hpk4Header{}, fmt.Errorf("archive integrity check failed")
	}
	return h, nil
}

func createPackageArchiveStreaming(entries []archiveSource, outPath string, level int, progress progressFunc) error {
	dir := filepath.Dir(outPath)
	tmp, err := writePackageTemp(entries, dir, progress)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return compressFileStreaming(tmp, outPath, level, progress)
}

func extractArchiveStreaming(src, dst string, progress progressFunc) (string, int, error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".hyperpack-raw-*.tmp")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := decompressHpkStreaming(src, tmp, progress); err != nil {
		_ = tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		return "", 0, err
	}
	var magicBuf [4]byte
	_, err = io.ReadFull(f, magicBuf[:])
	_ = f.Close()
	if err != nil {
		return "", 0, err
	}
	if binary.LittleEndian.Uint32(magicBuf[:]) == archiveMagic {
		count, err := extractPackageFile(tmpPath, dst, progress)
		if err != nil {
			return "", 0, err
		}
		return fmt.Sprintf("압축 해제 완료\n\n항목 수: %d\n\n%s", count, dst), count, nil
	}
	name := filepath.Base(src)
	if strings.HasSuffix(strings.ToLower(name), ".hpk") {
		name = name[:len(name)-4]
	}
	if name == "" {
		name = "restored"
	}
	target := filepath.Join(dst, name+".bin")
	if err := copyFile(tmpPath, target); err != nil {
		return "", 0, err
	}
	return "압축 해제 완료\n\n복원 파일:\n" + target, 1, nil
}

func crcAndSizeFilePath(path string) (uint32, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	buf := make([]byte, 1<<20)
	var n int64
	for {
		readN, er := f.Read(buf)
		if readN > 0 {
			_, _ = h.Write(buf[:readN])
			n += int64(readN)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return 0, 0, er
		}
	}
	return h.Sum32(), n, nil
}

func extractPackageFile(rawPath, dst string, progress progressFunc) (int, error) {
	in, err := os.Open(rawPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	var hdr [12]byte
	if _, err := io.ReadFull(in, hdr[:]); err != nil {
		return 0, err
	}
	if binary.LittleEndian.Uint32(hdr[0:4]) != archiveMagic || binary.LittleEndian.Uint16(hdr[4:6]) != archiveVersion {
		return 0, fmt.Errorf("unsupported HyperPack package")
	}
	count := binary.LittleEndian.Uint32(hdr[8:12])
	if count > 1_000_000 {
		return 0, fmt.Errorf("invalid entry count")
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return 0, err
	}
	for i := uint32(0); i < count; i++ {
		var eh [17]byte
		if _, err := io.ReadFull(in, eh[:]); err != nil {
			return 0, err
		}
		kind := eh[0]
		pathLen := int(binary.LittleEndian.Uint32(eh[1:5]))
		size := binary.LittleEndian.Uint64(eh[5:13])
		wantCRC := binary.LittleEndian.Uint32(eh[13:17])
		if pathLen < 0 || pathLen > 1<<20 {
			return 0, fmt.Errorf("invalid package path")
		}
		pb := make([]byte, pathLen)
		if _, err := io.ReadFull(in, pb); err != nil {
			return 0, err
		}
		path := string(pb)
		if !safeArchivePath(path) {
			return 0, fmt.Errorf("unsafe package path: %s", path)
		}
		target := filepath.Join(dst, filepath.FromSlash(path))
		if kind == 1 {
			if size != 0 || wantCRC != 0 {
				return 0, fmt.Errorf("invalid directory entry: %s", path)
			}
			if err := os.MkdirAll(target, 0755); err != nil {
				return 0, err
			}
		} else if kind == 0 {
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return 0, err
			}
			out, err := os.Create(target)
			if err != nil {
				return 0, err
			}
			crc := crc32.NewIEEE()
			copied, copyErr := io.Copy(io.MultiWriter(out, crc), io.LimitReader(in, int64(size)))
			closeErr := out.Close()
			if copyErr != nil {
				return 0, copyErr
			}
			if closeErr != nil {
				return 0, closeErr
			}
			if copied != int64(size) {
				return 0, fmt.Errorf("truncated package file: %s", path)
			}
			if crc.Sum32() != wantCRC {
				return 0, fmt.Errorf("CRC mismatch: %s", path)
			}
		} else {
			return 0, fmt.Errorf("invalid package entry type")
		}
		if progress != nil {
			progress(int64(i+1), int64(count), "압축 해제")
		}
	}
	var one [1]byte
	if n, err := in.Read(one[:]); err != io.EOF || n != 0 {
		return 0, fmt.Errorf("trailing bytes in package")
	}
	return int(count), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func runCLI(args []string) int {
	if len(args) == 0 {
		return -1
	}
	switch strings.ToLower(args[0]) {
	case "--pack-files":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: --pack-files output.hpk [--level N] file1 [file2 ...]")
			return 2
		}
		out := args[1]
		level := 9
		start := 2
		if len(args) >= 4 && args[2] == "--level" {
			v, e := strconv.Atoi(args[3])
			if e != nil || v < 0 || v > 9 {
				fmt.Fprintln(os.Stderr, "invalid level")
				return 2
			}
			level = v
			start = 4
		}
		if len(args) <= start {
			fmt.Fprintln(os.Stderr, "no files selected")
			return 2
		}
		paths := args[start:]
		entries, err := collectFileSources(paths)
		if err == nil {
			err = createPackageArchiveStreaming(entries, out, level, nil)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("packed %d files -> %s\n", len(entries), out)
		return 0
	case "--pack-folder":
		if len(args) < 3 || len(args) > 5 {
			fmt.Fprintln(os.Stderr, "usage: --pack-folder output.hpk folder [--level N]")
			return 2
		}
		out, root := args[1], args[2]
		level := 9
		if len(args) == 5 && args[3] == "--level" {
			v, e := strconv.Atoi(args[4])
			if e != nil || v < 0 || v > 9 {
				fmt.Fprintln(os.Stderr, "invalid level")
				return 2
			}
			level = v
		}
		entries, err := collectTreeSources(root)
		if err == nil {
			err = createPackageArchiveStreaming(entries, out, level, nil)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("packed folder %s -> %s\n", root, out)
		return 0
	case "--extract":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: --extract archive.hpk output_dir_or_file")
			return 2
		}
		archivePath, out := args[1], args[2]
		data, err := os.ReadFile(archivePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		raw, err := decompressBytes(data)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		entries, isPkg, err := decodePackage(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if isPkg {
			if err := extractPackage(entries, out); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fmt.Printf("extracted %d entries -> %s\n", len(entries), out)
		} else {
			if err := os.WriteFile(out, raw, 0644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fmt.Printf("extracted -> %s\n", out)
		}
		return 0
	case "--compress":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: --compress input output.hpk")
			return 2
		}
		if err := compressFileStreaming(args[1], args[2], 7, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "--decompress":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: --decompress input.hpk output")
			return 2
		}
		out, err := os.Create(args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_, err = decompressHpkStreaming(args[1], out, nil)
		closeErr := out.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if closeErr != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	return -1
}

// ----------------------------- Stable Win32 GUI -----------------------------
// v0.2.7: rebuilt the startup/UI path around plain Win32 controls.
// No owner-draw buttons, no custom WM_PAINT, and no custom GDI font creation
// during startup. The heavy compressor still runs in a hidden child process.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procEnableWindow         = user32.NewProc("EnableWindow")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procInvalidateRect       = user32.NewProc("InvalidateRect")
	procSetTimer             = user32.NewProc("SetTimer")
	procKillTimer            = user32.NewProc("KillTimer")
	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	procGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileNameW     = comdlg32.NewProc("GetSaveFileNameW")
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
	procCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
)

const (
	wsOverlapped        = 0x00000000
	wsCaption           = 0x00C00000
	wsSysMenu           = 0x00080000
	wsMinimizeBox       = 0x00020000
	wsChild             = 0x40000000
	wsVisible           = 0x10000000
	wsTabstop           = 0x00010000
	bsPushButton        = 0x00000000
	bsGroupBox          = 0x00000007
	cbsDropDownList     = 0x0003
	ssLeft              = 0x00000000
	ssCenter            = 0x00000001
	ssSimple            = 0x0000000B
	wmDestroy           = 0x0002
	wmCtlColorStatic    = 0x0138
	wmCommand           = 0x0111
	wmAppDone           = 0x8001
	wmAppProgress       = 0x8002
	wmTimer             = 0x0113
	wmSetFont           = 0x0030
	swShow              = 5
	mbOK                = 0x0
	mbIconInfo          = 0x40
	mbIconError         = 0x10
	cbGetCurSel         = 0x0147
	cbAddString         = 0x0143
	cbSetCurSel         = 0x014E
	transparent         = 1
	ofnExplorer         = 0x00080000
	ofnFileMustExist    = 0x00001000
	ofnPathMustExist    = 0x00000800
	ofnAllowMulti       = 0x00000200
	ofnNoChangeDir      = 0x00000008
	bifReturnOnlyFSDirs = 0x0001
	bifNewDialogStyle   = 0x0040
)

const (
	colWindow uint32 = 0x00F4F7FB // RGB 251,247,244-ish in BGR
	colHeader uint32 = 0x00402A16
	colText   uint32 = 0x001F2933
	colMuted  uint32 = 0x005A6773
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type point struct{ X, Y int32 }
type msg struct {
	HWnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type openFileName struct {
	lStructSize       uint32
	hwndOwner         syscall.Handle
	hInstance         syscall.Handle
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

type browseInfo struct {
	hwndOwner      syscall.Handle
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

var (
	mainWnd                                               syscall.Handle
	headerWnd                                             syscall.Handle
	statusWnd                                             syscall.Handle
	btnFiles, btnFolder, btnExtract, btnAbout, comboLevel syscall.Handle
	labelSubtitle, labelCPU, labelLevel, labelTip         syscall.Handle
	windowBrush, headerBrush                              syscall.Handle
	busy                                                  bool
	opDone                                                = make(chan string, 4)
	selectedLevel                                         = 9
	uiWndProcPtr                                          uintptr
	uiClassName                                           []uint16
	uiTimerID                                             uintptr = 77
	workerStatusPath                                      string
	workerResultPath                                      string
	workerManifestPath                                    string
	startupLogPath                                        string
	progressMu                                            sync.Mutex
	progressText                                          = "준비됨"
	progressPct                                           = 0
)

func utf16Ptr(s string) *uint16 { b := utf16.Encode([]rune(s)); b = append(b, 0); return &b[0] }
func utf16z(s string) []uint16  { b := utf16.Encode([]rune(s)); return append(b, 0) }

func appendStartupLog(line string) {
	if startupLogPath == "" {
		startupLogPath = filepath.Join(os.TempDir(), "HyperPack_startup.log")
	}
	f, err := os.OpenFile(startupLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %s\r\n", time.Now().Format("15:04:05.000"), line)
	_ = f.Close()
}

func guiMessage(hwnd syscall.Handle, title, text string, flags uintptr) {
	procMessageBoxW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), flags)
}

func setWindowText(hwnd syscall.Handle, text string) {
	if hwnd != 0 {
		procSendMessageW.Call(uintptr(hwnd), 0x000C, 0, uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
}

func setProgressText(text string) {
	pct := progressPct
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == '%' {
			j := i - 1
			for j >= 0 && text[j] >= '0' && text[j] <= '9' {
				j--
			}
			if j < i-1 {
				if v, err := strconv.Atoi(text[j+1 : i]); err == nil {
					pct = v
				}
			}
			break
		}
	}
	progressMu.Lock()
	progressText, progressPct = text, pct
	progressMu.Unlock()
	if statusWnd != 0 {
		setWindowText(statusWnd, text)
	}
}
func setProgress(done, total int64, stage string, workers int) {
	pct := 0
	if total > 0 {
		pct = int(done * 100 / total)
	}
	if pct > 100 {
		pct = 100
	}
	setProgressText(fmt.Sprintf("%s  %d%%   ·   CPU %d개", stage, pct, workers))
}

func readProgress() (string, int) {
	progressMu.Lock()
	defer progressMu.Unlock()
	return progressText, progressPct
}

func selectedCompressionLevel() int {
	r, _, _ := procSendMessageW.Call(uintptr(comboLevel), cbGetCurSel, 0, 0)
	if int(r) >= 0 && int(r) <= 9 {
		selectedLevel = int(r)
	}
	return selectedLevel
}

func setBusyText(text string) {
	busy = true
	for _, h := range []syscall.Handle{btnFiles, btnFolder, btnExtract, btnAbout, comboLevel} {
		if h != 0 {
			procEnableWindow.Call(uintptr(h), 0)
		}
	}
	setProgressText(text)
}
func finishOperation() {
	busy = false
	for _, h := range []syscall.Handle{btnFiles, btnFolder, btnExtract, btnAbout, comboLevel} {
		if h != 0 {
			procEnableWindow.Call(uintptr(h), 1)
		}
	}
}

func makeControl(parent syscall.Handle, className, text string, style uint32, x, y, w, h, id int) syscall.Handle {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	c, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(style), uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(w)), uintptr(int32(h)),
		uintptr(parent), uintptr(id), hInst, 0)
	return syscall.Handle(c)
}

func makeButton(parent syscall.Handle, text string, x, y, w, h, id int) syscall.Handle {
	return makeControl(parent, "BUTTON", text, wsChild|wsVisible|wsTabstop|bsPushButton, x, y, w, h, id)
}
func makeStatic(parent syscall.Handle, text string, x, y, w, h, id int) syscall.Handle {
	return makeControl(parent, "STATIC", text, wsChild|wsVisible|ssLeft, x, y, w, h, id)
}

func wndProc(hwnd syscall.Handle, uMsg uint32, wParam, lParam uintptr) uintptr {
	switch uMsg {
	case wmCtlColorStatic:
		child := syscall.Handle(lParam)
		if child == headerWnd {
			return uintptr(headerBrush)
		}
		return uintptr(windowBrush)
	case wmTimer:
		if wParam == uiTimerID {
			pollWorkerStatus()
		}
		return 0
	case wmCommand:
		id := uint16(wParam & 0xFFFF)
		switch id {
		case 1001:
			if !busy {
				chooseFiles(hwnd)
			}
		case 1002:
			if !busy {
				chooseFolder(hwnd)
			}
		case 1003:
			if !busy {
				chooseExtract(hwnd)
			}
		case 1004:
			guiMessage(hwnd, "HyperPack 0.3.2 Xtreme+", "HyperPack\n\nXtreme 고압축 멀티파일 / 폴더 패키저\n\n• 여러 파일 묶기\n• 폴더 구조 보존\n• 256MiB dictionary · 최대 1MiB match\n• 압축 작업을 별도 프로세스로 실행\n• UI 스레드와 작업 스레드 분리", mbOK|mbIconInfo)
		}
		return 0
	case wmAppDone:
		finishOperation()
		select {
		case r := <-opDone:
			if strings.HasPrefix(r, "!") {
				setProgressText("실패: " + strings.TrimPrefix(r, "!"))
				guiMessage(hwnd, "HyperPack 오류", strings.TrimPrefix(r, "!"), mbOK|mbIconError)
			} else {
				setProgressText(r)
			}
		default:
		}
		if workerStatusPath != "" {
			_ = os.Remove(workerStatusPath)
		}
		if workerResultPath != "" {
			_ = os.Remove(workerResultPath)
		}
		if workerManifestPath != "" {
			_ = os.Remove(workerManifestPath)
		}
		workerStatusPath, workerResultPath, workerManifestPath = "", "", ""
		return 0
	case wmDestroy:
		procKillTimer.Call(uintptr(hwnd), uiTimerID)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(uMsg), wParam, lParam)
	return r
}

func writeManifest(paths []string) (string, error) {
	f, err := os.CreateTemp("", "hyperpack-manifest-*.txt")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	w := bufio.NewWriter(f)
	for _, p := range paths {
		if _, err := w.WriteString(p + "\n"); err != nil {
			return "", err
		}
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
func readManifest(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var out []string
	for sc.Scan() {
		if sc.Text() != "" {
			out = append(out, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func writeWorkerStatus(path, stage string, done, total int64, workers int) {
	if path == "" {
		return
	}
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%s\t%d\t%d\t%d\n", stage, done, total, workers)), 0600)
}
func workerProgress(statusPath string) progressFunc {
	return func(done, total int64, stage string) {
		writeWorkerStatus(statusPath, stage, done, total, workerCount())
	}
}
func writeWorkerResult(path string, ok bool, msg string) {
	if path == "" {
		return
	}
	prefix := "ERR\n"
	if ok {
		prefix = "OK\n"
	}
	_ = os.WriteFile(path, []byte(prefix+msg), 0600)
}
func workerResult(path string) (bool, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, "작업 결과를 읽을 수 없습니다."
	}
	s := string(b)
	if strings.HasPrefix(s, "OK\n") {
		return true, strings.TrimSpace(strings.TrimPrefix(s, "OK\n"))
	}
	if strings.HasPrefix(s, "ERR\n") {
		return false, strings.TrimSpace(strings.TrimPrefix(s, "ERR\n"))
	}
	return false, "알 수 없는 작업 결과입니다."
}

func workerMain(args []string) int {
	if len(args) < 1 || !strings.HasPrefix(args[0], "--worker-") {
		return -1
	}
	switch args[0] {
	case "--worker-pack-files":
		if len(args) != 6 {
			return 2
		}
		manifest, out, levelStr, statusPath, resultPath := args[1], args[2], args[3], args[4], args[5]
		level, e := strconv.Atoi(levelStr)
		if e != nil || level < 0 || level > 9 {
			writeWorkerResult(resultPath, false, "압축 레벨이 잘못되었습니다.")
			return 2
		}
		paths, e := readManifest(manifest)
		if e == nil {
			var entries []archiveSource
			entries, e = collectFileSources(paths)
			if e == nil {
				e = createPackageArchiveStreaming(entries, out, level, workerProgress(statusPath))
				if e == nil {
					writeWorkerResult(resultPath, true, fmt.Sprintf("파일 %d개 압축 완료 · Level %d", len(entries), level))
					return 0
				}
			}
		}
		writeWorkerResult(resultPath, false, e.Error())
		return 1
	case "--worker-pack-folder":
		if len(args) != 6 {
			return 2
		}
		root, out, levelStr, statusPath, resultPath := args[1], args[2], args[3], args[4], args[5]
		level, e := strconv.Atoi(levelStr)
		if e != nil || level < 0 || level > 9 {
			writeWorkerResult(resultPath, false, "압축 레벨이 잘못되었습니다.")
			return 2
		}
		entries, e := collectTreeSources(root)
		if e == nil {
			e = createPackageArchiveStreaming(entries, out, level, workerProgress(statusPath))
			if e == nil {
				files, dirs := 0, 0
				for _, x := range entries {
					if x.Kind == "file" {
						files++
					} else {
						dirs++
					}
				}
				writeWorkerResult(resultPath, true, fmt.Sprintf("폴더 압축 완료 · 파일 %d개 · 폴더 %d개 · Level %d", files, dirs, level))
				return 0
			}
		}
		writeWorkerResult(resultPath, false, e.Error())
		return 1
	case "--worker-extract":
		if len(args) != 5 {
			return 2
		}
		src, dst, statusPath, resultPath := args[1], args[2], args[3], args[4]
		msg, _, e := extractArchiveStreaming(src, dst, workerProgress(statusPath))
		if e == nil {
			writeWorkerResult(resultPath, true, msg)
			return 0
		}
		writeWorkerResult(resultPath, false, e.Error())
		return 1
	}
	return -1
}

func launchWorker(args []string, statusPath, resultPath, startText string) {
	setBusyText(startText)
	appendStartupLog("launchWorker: " + strings.Join(args, " "))
	exe, err := os.Executable()
	if err != nil {
		opDone <- "!실행 파일 경로를 확인할 수 없습니다: " + err.Error()
		procPostMessageW.Call(uintptr(mainWnd), wmAppDone, 0, 0)
		return
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		opDone <- "!작업 프로세스를 시작하지 못했습니다: " + err.Error()
		procPostMessageW.Call(uintptr(mainWnd), wmAppDone, 0, 0)
		return
	}
	workerStatusPath = statusPath
	workerResultPath = resultPath
	if len(args) > 1 && strings.HasPrefix(args[0], "--worker-pack-files") {
		workerManifestPath = args[1]
	}
	go func() {
		err := cmd.Wait()
		ok, msg := workerResult(resultPath)
		if err != nil || !ok {
			if msg == "" {
				msg = "작업 프로세스가 오류와 함께 종료되었습니다."
			}
			opDone <- "!" + msg
		} else {
			opDone <- msg
		}
		procPostMessageW.Call(uintptr(mainWnd), wmAppDone, 0, 0)
	}()
}

func pollWorkerStatus() {
	if !busy || workerStatusPath == "" {
		return
	}
	b, err := os.ReadFile(workerStatusPath)
	if err != nil {
		return
	}
	parts := strings.Split(strings.TrimSpace(string(b)), "\t")
	if len(parts) < 4 {
		return
	}
	done, _ := strconv.ParseInt(parts[1], 10, 64)
	total, _ := strconv.ParseInt(parts[2], 10, 64)
	workers, _ := strconv.Atoi(parts[3])
	setProgress(done, total, parts[0], workers)
}
func tempStatusFiles() (string, string) {
	a, _ := os.CreateTemp(os.TempDir(), "hyperpack-status-*.txt")
	b, _ := os.CreateTemp(os.TempDir(), "hyperpack-result-*.txt")
	sp, rp := "", ""
	if a != nil {
		sp = a.Name()
		_ = a.Close()
		_ = os.Remove(sp)
	}
	if b != nil {
		rp = b.Name()
		_ = b.Close()
		_ = os.Remove(rp)
	}
	return sp, rp
}

func chooseFiles(hwnd syscall.Handle) {
	paths, ok := openMultiDialog(hwnd, "묶어서 압축할 파일 선택")
	if !ok || len(paths) == 0 {
		return
	}
	dst, ok := saveDialog(hwnd, "HyperPack 파일 저장", "hpk", "HyperPack archives\x00*.hpk\x00All files\x00*.*\x00")
	if !ok {
		return
	}
	manifest, err := writeManifest(paths)
	if err != nil {
		guiMessage(hwnd, "HyperPack 오류", err.Error(), mbOK|mbIconError)
		return
	}
	statusPath, resultPath := tempStatusFiles()
	level := selectedCompressionLevel()
	launchWorker([]string{"--worker-pack-files", manifest, dst, strconv.Itoa(level), statusPath, resultPath}, statusPath, resultPath, fmt.Sprintf("파일 %d개 · Level %d · 작업 시작", len(paths), level))
}
func chooseFolder(hwnd syscall.Handle) {
	src, ok := browseFolder(hwnd, "압축할 폴더 선택")
	if !ok {
		return
	}
	dst, ok := saveDialog(hwnd, "HyperPack 파일 저장", "hpk", "HyperPack archives\x00*.hpk\x00All files\x00*.*\x00")
	if !ok {
		return
	}
	statusPath, resultPath := tempStatusFiles()
	level := selectedCompressionLevel()
	launchWorker([]string{"--worker-pack-folder", src, dst, strconv.Itoa(level), statusPath, resultPath}, statusPath, resultPath, fmt.Sprintf("폴더 압축 · Level %d · 작업 시작", level))
}
func chooseExtract(hwnd syscall.Handle) {
	src, ok := openDialog(hwnd, "열 HyperPack 파일 선택", "HyperPack archives\x00*.hpk\x00All files\x00*.*\x00")
	if !ok {
		return
	}
	dst, ok := browseFolder(hwnd, "압축을 풀 폴더 선택")
	if !ok {
		return
	}
	statusPath, resultPath := tempStatusFiles()
	launchWorker([]string{"--worker-extract", src, dst, statusPath, resultPath}, statusPath, resultPath, "압축 해제 · 작업 시작")
}

func makeFileDialogCommon(hwnd syscall.Handle, title, filter string) (openFileName, []uint16) {
	buf := make([]uint16, 32768)
	fbuf := utf16z(filter)
	tbuf := utf16z(title)
	d := openFileName{lStructSize: uint32(unsafe.Sizeof(openFileName{})), hwndOwner: hwnd, lpstrFilter: &fbuf[0], nFilterIndex: 1, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)), lpstrTitle: &tbuf[0], flags: ofnExplorer | ofnPathMustExist | ofnNoChangeDir}
	return d, buf
}
func openDialog(hwnd syscall.Handle, title, filter string) (string, bool) {
	d, buf := makeFileDialogCommon(hwnd, title, filter)
	d.flags |= ofnFileMustExist
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&d)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}
func openMultiDialog(hwnd syscall.Handle, title string) ([]string, bool) {
	d, _ := makeFileDialogCommon(hwnd, title, "All files\x00*.*\x00")
	d.flags |= ofnFileMustExist | ofnAllowMulti
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&d)))
	if r == 0 {
		return nil, false
	}
	raw := unsafe.Slice(d.lpstrFile, int(d.nMaxFile))
	var parts []string
	start := 0
	for start < len(raw) {
		end := start
		for end < len(raw) && raw[end] != 0 {
			end++
		}
		if end == start {
			break
		}
		parts = append(parts, syscall.UTF16ToString(raw[start:end]))
		start = end + 1
	}
	if len(parts) == 1 {
		return []string{parts[0]}, true
	}
	dir := parts[0]
	out := make([]string, 0, len(parts)-1)
	for _, name := range parts[1:] {
		out = append(out, filepath.Join(dir, name))
	}
	return out, true
}
func saveDialog(hwnd syscall.Handle, title, defExt, filter string) (string, bool) {
	d, _ := makeFileDialogCommon(hwnd, title, filter)
	fbuf := utf16z(filter)
	tbuf := utf16z(title)
	d.lpstrFilter = &fbuf[0]
	d.lpstrTitle = &tbuf[0]
	d.lpstrDefExt = utf16Ptr(defExt)
	r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&d)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(unsafe.Slice(d.lpstrFile, int(d.nMaxFile))), true
}
func browseFolder(hwnd syscall.Handle, title string) (string, bool) {
	display := make([]uint16, 32768)
	path := make([]uint16, 32768)
	bi := browseInfo{hwndOwner: hwnd, pszDisplayName: &display[0], lpszTitle: utf16Ptr(title), ulFlags: bifReturnOnlyFSDirs | bifNewDialogStyle}
	r, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if r == 0 {
		return "", false
	}
	pidl := r
	defer procCoTaskMemFree.Call(pidl)
	ok, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if ok == 0 {
		return "", false
	}
	return syscall.UTF16ToString(path), true
}

func runGUI() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	appendStartupLog("runGUI: begin")

	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		appendStartupLog("GetModuleHandleW failed")
		guiMessage(0, "HyperPack", "프로그램 초기화에 실패했습니다.", mbOK|mbIconError)
		return
	}
	uiClassName = utf16z("HyperPackXtreme032")
	uiWndProcPtr = syscall.NewCallback(wndProc)
	windowBrush = makeBrush(colWindow)
	headerBrush = makeBrush(colHeader)
	if windowBrush == 0 || headerBrush == 0 {
		appendStartupLog("CreateSolidBrush failed")
		guiMessage(0, "HyperPack", "GUI 초기화에 실패했습니다.", mbOK|mbIconError)
		return
	}
	defer procDeleteObject.Call(uintptr(windowBrush))
	defer procDeleteObject.Call(uintptr(headerBrush))

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         0,
		lpfnWndProc:   uiWndProcPtr,
		hInstance:     syscall.Handle(hInst),
		hbrBackground: windowBrush,
		lpszClassName: &uiClassName[0],
	}
	appendStartupLog("RegisterClassExW")
	r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		appendStartupLog("RegisterClassExW failed: " + e.Error())
		guiMessage(0, "HyperPack", "창 클래스 등록에 실패했습니다.\n\n"+e.Error(), mbOK|mbIconError)
		return
	}

	titleBuf := utf16z("HyperPack 0.3.2 Xtreme+")
	appendStartupLog("CreateWindowExW")
	hwnd, _, e := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&uiClassName[0])),
		uintptr(unsafe.Pointer(&titleBuf[0])),
		uintptr(wsOverlapped|wsCaption|wsSysMenu|wsMinimizeBox),
		120, 80, 820, 520, 0, 0, hInst, 0)
	if hwnd == 0 {
		appendStartupLog("CreateWindowExW failed: " + e.Error())
		guiMessage(0, "HyperPack", "메인 창 생성에 실패했습니다.\n\n"+e.Error(), mbOK|mbIconError)
		return
	}
	mainWnd = syscall.Handle(hwnd)

	appendStartupLog("Create child controls")
	headerWnd = makeStatic(mainWnd, "HYPERPACK", 30, 24, 740, 42, 4001)
	labelSubtitle = makeStatic(mainWnd, "XTREME+ 고압축 · Solid · CPU/RAM + 선택적 GPU 보조", 32, 70, 650, 24, 4002)
	labelCPU = makeStatic(mainWnd, fmt.Sprintf("논리 CPU %d개", workerCount()), 610, 70, 150, 24, 4003)

	btnFiles = makeButton(mainWnd, "파일 묶어서 압축", 50, 120, 220, 80, 1001)
	btnFolder = makeButton(mainWnd, "폴더 전체 압축", 300, 120, 220, 80, 1002)
	btnExtract = makeButton(mainWnd, "HPK 압축 해제", 550, 120, 220, 80, 1003)

	labelLevel = makeStatic(mainWnd, "압축 레벨", 54, 230, 90, 25, 4004)
	comboLevel = makeControl(mainWnd, "COMBOBOX", "", wsChild|wsVisible|wsTabstop|cbsDropDownList, 145, 225, 150, 180, 2001)
	for i := 0; i <= 9; i++ {
		txt := utf16Ptr(fmt.Sprintf("Level %d", i))
		procSendMessageW.Call(uintptr(comboLevel), cbAddString, 0, uintptr(unsafe.Pointer(txt)))
	}
	procSendMessageW.Call(uintptr(comboLevel), cbSetCurSel, 9, 0)
	labelTip = makeStatic(mainWnd, "Level 9 Xtreme+ = CPU / RAM 적극 사용 · GPU 자동 보조", 320, 230, 360, 25, 4005)
	statusWnd = makeStatic(mainWnd, "준비됨", 50, 275, 720, 60, 4006)
	btnAbout = makeButton(mainWnd, "정보", 50, 375, 100, 40, 1004)
	footer := makeStatic(mainWnd, "HPK4 Xtreme+ · 64MiB dictionary · 64KiB+ match · 선택적 GPU hash assist", 180, 378, 560, 32, 4007)
	_ = footer

	procShowWindow.Call(uintptr(hwnd), swShow)
	procUpdateWindow.Call(uintptr(hwnd))
	procSetTimer.Call(uintptr(hwnd), uiTimerID, 200, 0)
	appendStartupLog("message loop")

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	appendStartupLog("runGUI: end")
}

func makeBrush(color uint32) syscall.Handle {
	b, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return syscall.Handle(b)
}

func main() {
	if len(os.Args) > 1 {
		if code := workerMain(os.Args[1:]); code >= 0 {
			os.Exit(code)
		}
		if code := runCLI(os.Args[1:]); code >= 0 {
			os.Exit(code)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			guiMessage(0, "HyperPack 오류", "예기치 않은 오류가 발생했습니다.\n\n"+fmt.Sprint(r), mbOK|mbIconError)
		}
	}()
	runGUI()
}
