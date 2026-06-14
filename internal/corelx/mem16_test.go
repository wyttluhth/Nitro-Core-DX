package corelx

import "testing"

// TestMem16WRAM verifies mem.write16/read16 do true 16-bit little-endian
// access in WRAM, and that mem.read16 reads back what mem.write16 wrote.
func TestMem16WRAM(t *testing.T) {
	source := `var roundtrip: int = 0
function Start()
    mem.write16(0x7400, 0xBEEF)
    roundtrip = mem.read16(0x7400)
    while true
        wait_vblank()
`
	emu, result := compileAndBoot(t, source, 600)
	var addr uint16
	for _, e := range result.MemoryMap {
		if e.Name == "roundtrip" {
			addr = e.Address
		}
	}
	// Direct little-endian bytes in WRAM at 0x7400.
	if lo := emu.CPU.Mem.Read8(0, 0x7400); lo != 0xEF {
		t.Errorf("low byte at 0x7400: want 0xEF, got 0x%02X", lo)
	}
	if hi := emu.CPU.Mem.Read8(0, 0x7401); hi != 0xBE {
		t.Errorf("high byte at 0x7401: want 0xBE, got 0x%02X", hi)
	}
	// read16 round-trip into a global.
	if got := read16(emu, addr); got != 0xBEEF {
		t.Errorf("mem.read16 round-trip: want 0xBEEF, got 0x%04X", got)
	}
}
