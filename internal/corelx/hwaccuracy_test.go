package corelx

import "testing"

// TestConstFoldMatchesRuntimeFixmul guards the most dangerous Go-quirk risk:
// a fixed product computed at COMPILE time (Go int64 fold) must be bit-identical
// to the same product computed at RUNTIME by the __fixmul routine on the
// 16-bit datapath. They use different algorithms (fold: signed >>8 toward -inf;
// fixmul: abs-then-resign toward zero), so negative products could diverge.
// If they do, a const and a variable computing the same thing disagree on
// hardware. This test pins them together.
func TestConstFoldMatchesRuntimeFixmul(t *testing.T) {
	cases := []struct {
		name   string
		a, b   string // decimal literals
	}{
		{"pos_pos", "1.5", "2.5"},
		{"neg_pos", "-1.5", "2.5"},
		{"pos_neg", "1.5", "-2.5"},
		{"neg_neg", "-1.5", "-2.5"},
		{"frac", "0.25", "0.25"},
		{"neg_frac", "-0.25", "0.75"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			source := `const FOLDED = ` + c.a + ` * ` + c.b + `
var rt_a: fixed = ` + c.a + `
var rt_b: fixed = ` + c.b + `
var folded: fixed = FOLDED
var runtime: fixed = 0.0

function Start()
    runtime = rt_a * rt_b
    while true
        wait_vblank()
`
			emu, result := compileAndBoot(t, source, 8000)
			addrs := map[string]uint16{}
			for _, e := range result.MemoryMap {
				addrs[e.Name] = e.Address
			}
			folded := read16(emu, addrs["folded"])
			runtime := read16(emu, addrs["runtime"])
			if folded != runtime {
				t.Errorf("%s: compile-time fold 0x%04X != runtime fixmul 0x%04X — hardware divergence",
					c.name, folded, runtime)
			}
		})
	}
}

// TestConstFoldNegativeTruncation forces a negative product whose magnitude
// is NOT a multiple of 1/256, where fold (toward -inf) and fixmul (toward
// zero) would disagree if not reconciled. 0.1*0.1 in 8.8: 25*25=625,
// 625>>8 = 2; negated = -2. A toward-(-inf) fold would give -3.
func TestConstFoldNegativeTruncation(t *testing.T) {
	source := `const FOLDED = -0.1 * 0.1
var a: fixed = -0.1
var b: fixed = 0.1
var folded: fixed = FOLDED
var runtime: fixed = 0.0
function Start()
    runtime = a * b
    while true
        wait_vblank()
`
	emu, result := compileAndBoot(t, source, 8000)
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	folded := read16(emu, addrs["folded"])
	runtime := read16(emu, addrs["runtime"])
	if folded != runtime {
		t.Errorf("negative truncating product: fold 0x%04X != runtime fixmul 0x%04X — "+
			"compile-time and hardware disagree on rounding direction", folded, runtime)
	}
}
