package corelx

import (
	"os"
	"path/filepath"
	"testing"

	"nitro-core-dx/internal/emulator"
)

// TestLoadBitmapRenders verifies the full external-image pathway: an image
// asset's bitmap is DMA'd from the ROM data region onto a matrix plane and
// renders real (multi-color) pixels under perspective projection. The .corelx
// source and the .cxasset live in the same project folder, as a real project
// would be organized.
func TestLoadBitmapRenders(t *testing.T) {
	dir := t.TempDir()
	// Copy the real park floor asset into the project folder next to main.corelx.
	assetSrc, err := os.ReadFile(`/home/aj/Documents/Development/Nitro-Core-DX/Games/NitroPackInDemo/corelx/park_floor.cxasset`)
	if err != nil {
		t.Fatalf("read fixture asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "park_floor.cxasset"), assetSrc, 0644); err != nil {
		t.Fatal(err)
	}
	src := `asset ParkFloor: image "park_floor.cxasset"

var cx: int = 512
var cy: int = 512
function Start()
    gfx.init_default_palettes()
    bg.enable(0)
    bg.bind_transform(0, 0)
    bg.set_priority(0, 2)
    matrix.enable(0)
    matrix.identity(0)
    matrix.set_center(0, 160, 100)
    matrix_plane.enable(0, 32)
    matrix_plane.load_bitmap(ParkFloor, 0)
    matrix_plane.set_projection(0, 1, 113)
    matrix_plane.set_depth(0, 0x0C00, 0xC000, 0x00C0)
    ppu.enable_display()
    while true
        wait_vblank()
        matrix_plane.set_camera(0, cx, cy, 0, 256)
`
	srcPath := filepath.Join(dir, "main.corelx")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	romData, err := os.ReadFile(filepath.Join(dir, "main.rom"))
	if err != nil {
		t.Fatal(err)
	}
	emu := emulator.NewEmulator()
	if err := emu.LoadROM(romData); err != nil {
		t.Fatalf("load ROM: %v", err)
	}
	_ = result
	emu.Start()
	emu.SetFrameLimit(false)
	for i := 0; i < 40; i++ {
		emu.RunFrame()
	}
	buf := emu.GetOutputBuffer()
	colors := map[uint32]int{}
	nz := 0
	for y := 115; y < 195; y++ {
		for x := 0; x < 320; x++ {
			px := buf[y*320+x]
			if px != 0 {
				nz++
				colors[px]++
			}
		}
	}
	t.Logf("floor: %d non-black pixels, %d distinct colors", nz, len(colors))
	if nz < 2000 {
		t.Errorf("bitmap floor barely rendering: %d pixels", nz)
	}
	if len(colors) < 4 {
		t.Errorf("expected a real multi-color image, got %d distinct colors (looks like a flat fill)", len(colors))
	}
}
