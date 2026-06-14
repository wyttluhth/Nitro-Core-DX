package corelx

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"nitro-core-dx/internal/rom"
)

// imageDataStartBank is the first ROM bank used for image bitmap data. Code
// occupies bank 1 (and must stay under this bank); images live above it so
// their bank+offset is known before codegen.
const imageDataStartBank = 2

// ImageAsset is a parsed external .cxasset bitmap image, placed in ROM.
type ImageAsset struct {
	Name        string
	PlaneSize   int      // 32, 64, or 128 (tiles per side)
	PaletteBank uint8    // CGRAM bank (0-15)
	Palette     []uint16 // RGB555 colors
	Bitmap      []byte   // 4bpp packed bitmap
	Bank        uint8    // ROM bank where Bitmap starts
	Offset      uint16   // ROM offset (0x8000-based) where Bitmap starts
}

// loadImageAssets reads and parses every `image` asset's external .cxasset
// file, lays the bitmaps out in the ROM data region starting at bank 2, and
// returns the assets (with bank/offset filled in) plus the concatenated data
// region bytes for ROMBuilder.SetDataRegion.
func loadImageAssets(program *Program, sourcePath string) (map[string]*ImageAsset, []byte, error) {
	srcDir := filepath.Dir(sourcePath)
	assets := make(map[string]*ImageAsset)
	var region []byte
	cursor := 0 // byte offset within the data region (bank 2, 0x8000 = cursor 0)

	for _, a := range program.Assets {
		if a.Type != "image" {
			continue
		}
		path := a.FilePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(srcDir, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("image asset %s: %w", a.Name, err)
		}
		img, err := parseCxAsset(a.Name, string(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("image asset %s (%s): %w", a.Name, path, err)
		}

		// Place the bitmap in the ROM data region.
		img.Bank = uint8(imageDataStartBank + cursor/rom.ROMBankSizeBytes)
		img.Offset = uint16(rom.ROMBankOffsetBase + (cursor % rom.ROMBankSizeBytes))
		region = append(region, img.Bitmap...)
		cursor += len(img.Bitmap)

		assets[a.Name] = img
	}

	// Orphan check: every .cxasset file in the project directory must be
	// referenced by an image asset declaration. A stray asset file (dead art,
	// or a typo'd reference that left the file behind) is a hard error.
	referenced := make(map[string]bool)
	for _, a := range program.Assets {
		if a.Type == "image" {
			referenced[filepath.Base(a.FilePath)] = true
		}
	}
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".cxasset") {
			continue
		}
		if !referenced[e.Name()] {
			return nil, nil, fmt.Errorf("asset file %q is in the project but not referenced by any code (orphan); remove it or add `asset <Name>: image %q`", e.Name(), e.Name())
		}
	}
	return assets, region, nil
}

// parseCxAsset parses the importer's .cxasset text format:
//
//	image Name:
//	    kind: bitmap_plane
//	    plane_size: 32
//	    palette_bank: 1
//	    palette: hex 0000 7fff ...
//	    data: hex
//	        a0 fa ...
func parseCxAsset(name, text string) (*ImageAsset, error) {
	img := &ImageAsset{Name: name}
	lines := strings.Split(text, "\n")
	inData := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		if inData {
			for _, tok := range strings.Fields(t) {
				v, err := strconv.ParseUint(tok, 16, 8)
				if err != nil {
					return nil, fmt.Errorf("bad data byte %q: %w", tok, err)
				}
				img.Bitmap = append(img.Bitmap, byte(v))
			}
			continue
		}
		switch {
		case strings.HasPrefix(t, "plane_size:"):
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(t, "plane_size:")))
			img.PlaneSize = n
		case strings.HasPrefix(t, "palette_bank:"):
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(t, "palette_bank:")))
			img.PaletteBank = uint8(n)
		case strings.HasPrefix(t, "palette: hex"):
			for _, tok := range strings.Fields(strings.TrimPrefix(t, "palette: hex")) {
				v, err := strconv.ParseUint(tok, 16, 16)
				if err != nil {
					return nil, fmt.Errorf("bad palette color %q: %w", tok, err)
				}
				img.Palette = append(img.Palette, uint16(v))
			}
		case strings.HasPrefix(t, "data: hex"):
			inData = true
		}
	}
	if img.PlaneSize != 32 && img.PlaneSize != 64 && img.PlaneSize != 128 {
		return nil, fmt.Errorf("plane_size must be 32, 64, or 128 (got %d)", img.PlaneSize)
	}
	if len(img.Bitmap) == 0 {
		return nil, fmt.Errorf("no bitmap data")
	}
	return img, nil
}
