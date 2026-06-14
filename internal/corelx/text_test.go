package corelx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTextDrawPortProtocol verifies text.draw drives the hardware text port
// exactly: each character is buffered (one command per char) and the port's X
// cursor auto-advances by 8 per character, starting from the given X. These
// are port-level assertions against the real PPU, not a "looks plausible"
// check. Color channels land in the port's R/G/B registers.
func TestTextDrawPortProtocol(t *testing.T) {
	source := `function Start()
    text.draw(40, 80, 255, 128, 64, "HELLO")
    while true
        wait_vblank()
`
	emu, _ := compileAndBoot(t, source, 600)

	// "HELLO" = 5 characters => 5 buffered text commands.
	if got := emu.PPU.GetTextCount(); got != 5 {
		t.Fatalf("text command count: want 5 (one per char), got %d", got)
	}
	// X started at 40 and must have advanced by 8 per char: 40 + 8*5 = 80.
	if emu.PPU.TextX != 80 {
		t.Errorf("text X auto-advance: want 80 (40 + 8*5), got %d", emu.PPU.TextX)
	}
	// Y and color channels landed in the port registers.
	if emu.PPU.TextY != 80 {
		t.Errorf("text Y: want 80, got %d", emu.PPU.TextY)
	}
	if emu.PPU.TextR != 255 || emu.PPU.TextG != 128 || emu.PPU.TextB != 64 {
		t.Errorf("text color: want (255,128,64), got (%d,%d,%d)", emu.PPU.TextR, emu.PPU.TextG, emu.PPU.TextB)
	}
}

// TestTextDrawRendersToFramebuffer steps a full frame and confirms the text
// actually reached the display buffer (non-background pixels appear). This is
// the framebuffer-tier check for the first visible CoreLX feature.
func TestTextDrawRendersToFramebuffer(t *testing.T) {
	source := `function Start()
    text.draw(100, 100, 255, 255, 255, "X")
    while true
        wait_vblank()
`
	emu, _ := compileLoadForTest(t, source)
	emu.Start()
	emu.SetFrameLimit(false)
	// Exactly one frame: Start() runs text.draw (buffering the char) and this
	// frame's endFrame composites it into the display buffer. (A later frame
	// would clear it again, since the text is only drawn once.)
	if err := emu.RunFrame(); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	buf := emu.GetOutputBuffer()
	nonZero := 0
	for _, px := range buf {
		if px != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Error("expected text.draw to produce visible (non-background) pixels in the framebuffer; got an empty frame")
	}
}

// TestStringOutsideTextDrawRejected confirms strings are labels, not values.
func TestStringOutsideTextDrawRejected(t *testing.T) {
	source := `var x: int = 0
function Start()
    x = "nope"
    while true
        wait_vblank()
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err == nil || !strings.Contains(err.Error(), "string") {
		t.Fatalf("expected string-misuse error, got: %v", err)
	}
}
