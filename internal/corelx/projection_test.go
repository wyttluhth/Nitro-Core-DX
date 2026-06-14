package corelx

import "testing"

// TestMatrixPlaneProjectionBuiltins verifies the tier-1 matrix-plane register
// builtins land their values in the correct plane's hardware state. These are
// generic transformation-plane controls (no game-specific "floor"/"billboard"
// concept); the test asserts directly against the PPU plane state.
func TestMatrixPlaneProjectionBuiltins(t *testing.T) {
	source := `function Start()
    matrix_plane.set_projection(1, 2, 113)
    matrix_plane.set_depth(1, 0x0C00, 0xC000, 0x0070)
    matrix_plane.set_camera(1, 512, 768, 0, 256)
    matrix_plane.set_surface(1, 512, 600, 0, 256, 0x4000)
    while true
        wait_vblank()
`
	emu, _ := compileAndBoot(t, source, 1500)
	pl := emu.PPU.MatrixPlanes[1]

	if pl.ProjectionMode != 2 {
		t.Errorf("ProjectionMode: want 2 (vertical quad), got %d", pl.ProjectionMode)
	}
	if pl.Horizon != 113 {
		t.Errorf("Horizon: want 113, got %d", pl.Horizon)
	}
	if pl.BaseDistance != 0x0C00 {
		t.Errorf("BaseDistance: want 0x0C00, got 0x%04X", pl.BaseDistance)
	}
	if pl.FocalLength != 0xC000 {
		t.Errorf("FocalLength: want 0xC000, got 0x%04X", pl.FocalLength)
	}
	if pl.WidthScale != 0x0070 {
		t.Errorf("WidthScale: want 0x0070, got 0x%04X", pl.WidthScale)
	}
	if pl.CameraX != 512 || pl.CameraY != 768 {
		t.Errorf("Camera: want (512,768), got (%d,%d)", pl.CameraX, pl.CameraY)
	}
	if pl.HeadingX != 0 || pl.HeadingY != 256 {
		t.Errorf("Heading: want (0,256), got (%d,%d)", pl.HeadingX, pl.HeadingY)
	}
	if pl.OriginX != 512 || pl.OriginY != 600 {
		t.Errorf("Origin: want (512,600), got (%d,%d)", pl.OriginX, pl.OriginY)
	}
	if pl.FacingX != 0 || pl.FacingY != 256 {
		t.Errorf("Facing: want (0,256), got (%d,%d)", pl.FacingX, pl.FacingY)
	}
	if pl.HeightScale != 0x4000 {
		t.Errorf("HeightScale: want 0x4000, got 0x%04X", pl.HeightScale)
	}
	// Plane 0 must be untouched (channel selection isolates writes).
	if emu.PPU.MatrixPlanes[0].ProjectionMode != 0 {
		t.Errorf("plane 0 leaked: ProjectionMode = %d, want 0", emu.PPU.MatrixPlanes[0].ProjectionMode)
	}
}
