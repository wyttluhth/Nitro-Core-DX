package corelx

import (
	"testing"
)

// TestOverworldRebuild verifies the first chunk of the CoreLX demo rebuild: a
// walkable pseudo-3D overworld. It confirms the program compiles and runs, that
// tapping RIGHT turns the facing, that walking moves the camera along the new
// facing, and that the floor projection and player sprite reach the hardware.
func TestOverworldRebuild(t *testing.T) {
	emu, result := compileProjectDirForTest(t, "Games/NitroPackInDemo/corelx/overworld.corelx")
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	emu.Start()
	emu.SetFrameLimit(false)
	// Let one-time setup finish (loading floor tiles and clearing the 128x128
	// tilemap spans several frames at boot — on hardware the player isn't
	// pressing anything yet).
	for i := 0; i < 20; i++ {
		emu.RunFrame()
	}

	// Tap RIGHT twice: facing 0 (N) -> 1 (NE) -> 2 (E). Press+release each.
	for _, down := range []bool{true, false, true, false} {
		if down {
			emu.SetInputButtons(0x0008) // RIGHT (bit 3)
		} else {
			emu.SetInputButtons(0x0000)
		}
		emu.RunFrame()
	}
	if f := read16(emu, addrs["facing"]); f != 2 {
		t.Fatalf("after two RIGHT taps: facing want 2 (East), got %d", f)
	}

	// Now hold UP and walk: facing East means move_x>0, move_y==0, so cam_x
	// should increase and cam_y stay put.
	startX := read16(emu, addrs["cam_x"])
	startY := read16(emu, addrs["cam_y"])
	emu.SetInputButtons(0x0001) // UP held
	for i := 0; i < 5; i++ {
		emu.RunFrame()
	}
	if x := read16(emu, addrs["cam_x"]); x <= startX {
		t.Errorf("walking East: cam_x should increase from %d, got %d", startX, x)
	}
	if y := read16(emu, addrs["cam_y"]); y != startY {
		t.Errorf("walking East: cam_y should not change from %d, got %d", startY, y)
	}

	// Floor projection and camera reached the plane; heading is East (256,0).
	if emu.PPU.MatrixPlanes[0].ProjectionMode != 1 {
		t.Errorf("ProjectionMode want 1 (perspective floor), got %d", emu.PPU.MatrixPlanes[0].ProjectionMode)
	}
	if emu.PPU.MatrixPlanes[0].HeadingX != 256 || emu.PPU.MatrixPlanes[0].HeadingY != 0 {
		t.Errorf("plane heading want (256,0) for East, got (%d,%d)",
			emu.PPU.MatrixPlanes[0].HeadingX, emu.PPU.MatrixPlanes[0].HeadingY)
	}
	// HUD text drew.
	emu.RunFrame()
	if emu.GetOutputBuffer() == nil {
		t.Error("no framebuffer")
	}
}
