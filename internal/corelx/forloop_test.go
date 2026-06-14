package corelx

import "testing"

// TestForLoopInclusiveAscending verifies the BASIC for...to loop runs with
// inclusive bounds (0..7 = 8 iterations) and the loop variable is usable in
// the body.
func TestForLoopInclusiveAscending(t *testing.T) {
	source := `var sum: int = 0
var iters: int = 0
function Start()
    for i = 0 to 7
        sum = sum + i
        iters = iters + 1
    while true
        wait_vblank()
`
	emu, result := compileAndBoot(t, source, 2000)
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	// inclusive 0..7: 8 iterations, sum 0+1+...+7 = 28
	if got := read16(emu, addrs["iters"]); got != 8 {
		t.Errorf("iterations: want 8 (inclusive 0..7), got %d", got)
	}
	if got := read16(emu, addrs["sum"]); got != 28 {
		t.Errorf("sum 0..7: want 28, got %d", got)
	}
}

// TestForLoopStepDescending verifies negative step counts down inclusively.
func TestForLoopStepDescending(t *testing.T) {
	source := `var count: int = 0
var last: int = 99
function Start()
    for i = 10 to 0 step -2
        count = count + 1
        last = i
    while true
        wait_vblank()
`
	emu, result := compileAndBoot(t, source, 2000)
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	// 10,8,6,4,2,0 = 6 iterations, last = 0
	if got := read16(emu, addrs["count"]); got != 6 {
		t.Errorf("descending count 10..0 step -2: want 6, got %d", got)
	}
	if got := read16(emu, addrs["last"]); got != 0 {
		t.Errorf("last value: want 0, got %d", got)
	}
}
