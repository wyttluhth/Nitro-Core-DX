package corelx

import (
	"testing"
)

// TestOverworldFloorRenders reproduces the visual bug: does the matrix floor
// actually draw pixels, or is the screen blank apart from text? We sample the
// middle of the screen (away from the HUD text rows) for non-black pixels.
func TestOverworldFloorRenders(t *testing.T) {
	emu, _ := compileProjectDirForTest(t, "Games/NitroPackInDemo/corelx/overworld.corelx")
	emu.Start()
	emu.SetFrameLimit(false)
	for i := 0; i < 30; i++ {
		emu.RunFrame()
	}
	buf := emu.GetOutputBuffer() // 320x200
	// Sample the floor region: rows 120..180 (below horizon ~113, above bottom
	// HUD text at y=184), columns across the middle.
	floorPixels := 0
	for y := 120; y < 180; y++ {
		for x := 0; x < 320; x++ {
			if buf[y*320+x] != 0 {
				floorPixels++
			}
		}
	}
	t.Logf("non-black pixels in floor region (rows 120-180): %d", floorPixels)
	if floorPixels < 1000 {
		t.Errorf("matrix floor barely rendering: %d pixels in the floor region (want a filled floor)", floorPixels)
	}
}
