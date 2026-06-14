package corelx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnaryAmpersandRejected confirms the address-of operator no longer exists
// (charter D8: structs are reference types). Binary bitwise & still works.
func TestUnaryAmpersandRejected(t *testing.T) {
	source := `var x: int = 0
function Start()
    x = &x
    while true
        wait_vblank()
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	os.WriteFile(srcPath, []byte(source), 0644)
	_, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err == nil {
		t.Fatal("expected parse error for unary & (address-of removed in v1)")
	}
}

// TestBitwiseAndStillWorks confirms binary & is unaffected.
func TestBitwiseAndStillWorks(t *testing.T) {
	source := `var r: int = 0
function Start()
    r = 0xFF & 0x0F
    while true
        wait_vblank()
`
	emu, result := compileAndBoot(t, source, 400)
	var addr uint16
	for _, e := range result.MemoryMap {
		if e.Name == "r" {
			addr = e.Address
		}
	}
	if got := read16(emu, addr); got != 0x0F {
		t.Errorf("0xFF & 0x0F: want 0x0F, got 0x%04X", got)
	}
}
