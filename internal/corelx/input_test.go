package corelx

import "testing"

// TestInputEdgeDetection verifies input.poll/held/pressed semantics against the
// real controller hardware path: held reflects the current frame, pressed fires
// only on the rising edge (down now, up last frame). With A held across two
// polls, pressed must be true on the first and false on the second.
func TestInputEdgeDetection(t *testing.T) {
	source := `var held1: int = 0
var pressed1: int = 0
var pressed2: int = 0
var held_b: int = 0
function Start()
    input.poll()
    held1 = input.held(A)
    pressed1 = input.pressed(A)
    held_b = input.held(B)
    input.poll()
    pressed2 = input.pressed(A)
    while true
        wait_vblank()
`
	emu, result := compileLoadForTest(t, source)
	emu.SetInputButtons(0x0010) // A held (bit 4) the whole time; B not held
	// Step enough instructions to execute both polls and the stores.
	for i := 0; i < 4000; i++ {
		if err := emu.CPU.ExecuteInstruction(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	if got := read16(emu, addrs["held1"]); got == 0 {
		t.Error("held(A) with A down: want nonzero, got 0")
	}
	if got := read16(emu, addrs["held_b"]); got != 0 {
		t.Errorf("held(B) with B up: want 0, got 0x%04X", got)
	}
	if got := read16(emu, addrs["pressed1"]); got == 0 {
		t.Error("pressed(A) on first poll (rising edge): want nonzero, got 0")
	}
	if got := read16(emu, addrs["pressed2"]); got != 0 {
		t.Errorf("pressed(A) on second poll (still held, not a new press): want 0, got 0x%04X", got)
	}
}
