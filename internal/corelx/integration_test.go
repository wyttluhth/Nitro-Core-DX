package corelx

import "testing"

// TestIntegrationMovingFloor builds a pseudo-3D matrix-floor program in pure
// CoreLX using everything in the language core so far: a tiles asset, matrix
// plane setup, global camera state, input polling, the projection/camera
// builtins, and HUD text. It then runs frames with UP held and confirms the
// camera moved and that movement reached the plane's hardware state. This is
// the integration proof that the pieces compose on the real machine.
func TestIntegrationMovingFloor(t *testing.T) {
	source := `asset Floor: tiles8 hex
    11 11 22 22 11 11 22 22
    11 11 22 22 11 11 22 22
    22 22 11 11 22 22 11 11
    22 22 11 11 22 22 11 11
    11 11 22 22 11 11 22 22
    11 11 22 22 11 11 22 22
    22 22 11 11 22 22 11 11
    22 22 11 11 22 22 11 11

const MOVE = 4

var cam_x: int = 512
var cam_y: int = 768

function Start()
    gfx.init_default_palettes()
    bg.enable(0)
    bg.bind_transform(0, 0)
    matrix.enable(0)
    matrix.identity(0)
    matrix_plane.enable(0, 128)
    matrix_plane.load_tiles(ASSET_Floor, 0, 0)
    matrix_plane.clear(0, 1, 0)
    matrix_plane.set_projection(0, 1, 113)
    matrix_plane.set_depth(0, 0x0C00, 0xC000, 0x00C0)
    ppu.enable_display()
    while true
        wait_vblank()
        input.poll()
        if input.held(UP)
            cam_y = cam_y - MOVE
        if cam_y < 64
            cam_y = 64
        if input.held(DOWN)
            cam_y = cam_y + MOVE
        if input.held(LEFT)
            cam_x = cam_x - MOVE
        if input.held(RIGHT)
            cam_x = cam_x + MOVE
        matrix_plane.set_camera(0, cam_x, cam_y, 0, 256)
        text.draw(8, 8, 255, 255, 255, "NITRO CORELX FLOOR")
`
	emu, result := compileLoadForTest(t, source)
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	emu.Start()
	emu.SetFrameLimit(false)
	emu.SetInputButtons(0x0001) // UP held (bit 0)

	for i := 0; i < 30; i++ {
		if err := emu.RunFrame(); err != nil {
			t.Fatalf("RunFrame %d: %v", i, err)
		}
	}

	camY := read16(emu, addrs["cam_y"])
	// Sustained UP drives cam_y down to the clamp floor of 64 (and the signed
	// `if cam_y < 64` comparison keeps it there).
	if camY != 64 {
		t.Errorf("cam_y under sustained UP: want clamped to 64, got %d", camY)
	}
	// The movement reached the plane's hardware camera state every frame.
	if emu.PPU.MatrixPlanes[0].CameraY != 64 {
		t.Errorf("plane CameraY: want 64 (synced to cam_y), got %d", emu.PPU.MatrixPlanes[0].CameraY)
	}
	// Projection configured as a perspective floor.
	if emu.PPU.MatrixPlanes[0].ProjectionMode != 1 {
		t.Errorf("ProjectionMode: want 1 (perspective), got %d", emu.PPU.MatrixPlanes[0].ProjectionMode)
	}
	t.Logf("integration OK: globals + input + signed clamp + projection sync all working (cam_y -> %d)", camY)
}
