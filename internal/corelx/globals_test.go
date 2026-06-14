package corelx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nitro-core-dx/internal/emulator"
)

// read16 reads a little-endian 16-bit value from bank-0 WRAM.
func read16(emu *emulator.Emulator, addr uint16) uint16 {
	lo := emu.CPU.Mem.Read8(0, addr)
	hi := emu.CPU.Mem.Read8(0, addr+1)
	return uint16(lo) | uint16(hi)<<8
}

// compileAndBoot compiles source, loads it, and steps the CPU until the
// program reaches its main loop (bounded by maxSteps).
func compileAndBoot(t *testing.T, source string, maxSteps int) (*emulator.Emulator, *CompileResult) {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	romPath := filepath.Join(dir, "main.rom")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := CompileProject(srcPath, &CompileOptions{OutputPath: romPath})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	romData, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatalf("read ROM: %v", err)
	}
	emu := emulator.NewEmulator()
	if err := emu.LoadROM(romData); err != nil {
		t.Fatalf("load ROM: %v", err)
	}
	for i := 0; i < maxSteps; i++ {
		if err := emu.CPU.ExecuteInstruction(); err != nil {
			t.Fatalf("CPU step %d: %v", i, err)
		}
	}
	return emu, result
}

func TestGlobalsAndConsts(t *testing.T) {
	source := `const BASE = 100
const DOUBLE = BASE * 2
const MASKED = 0x0F0F & 0x00FF

var score: int = BASE + 5
var lives: u8 = 3
var hiscore: int
var pinned at 0x7100: int = DOUBLE

function Start()
    score = score + DOUBLE
    hiscore = score
    while true
        score = score
`
	emu, result := compileAndBoot(t, source, 600)

	// Memory map should list the runtime block and all four globals.
	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}
	if addrs["__runtime"] != 0x2000 {
		t.Fatalf("runtime block expected at 0x2000, map: %+v", result.MemoryMap)
	}
	if addrs["score"] != 0x2100 {
		t.Errorf("score expected at 0x2100, got 0x%04X", addrs["score"])
	}
	if addrs["pinned"] != 0x7100 {
		t.Errorf("pinned expected at its pin 0x7100, got 0x%04X", addrs["pinned"])
	}

	// score = (BASE+5) + DOUBLE = 105 + 200 = 305
	if got := read16(emu, addrs["score"]); got != 305 {
		t.Errorf("score: want 305, got %d", got)
	}
	if got := emu.CPU.Mem.Read8(0, addrs["lives"]); got != 3 {
		t.Errorf("lives: want 3, got %d", got)
	}
	if got := read16(emu, addrs["hiscore"]); got != 305 {
		t.Errorf("hiscore (global write from Start): want 305, got %d", got)
	}
	if got := read16(emu, 0x7100); got != 200 {
		t.Errorf("pinned global at 0x7100: want 200, got %d", got)
	}

	// Memory map text artifact exists next to the ROM.
	if !strings.Contains(string(result.MemoryMapText), "pinned") {
		t.Errorf("memory map text missing pinned entry:\n%s", result.MemoryMapText)
	}
}

func TestGlobalPinOverlapRejected(t *testing.T) {
	source := `var a at 0x2080: int

function Start()
    while true
        a = 1
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err == nil {
		t.Fatal("expected pin-overlap error for global pinned inside the runtime block")
	}
	if !strings.Contains(err.Error(), "runtime block") {
		t.Errorf("expected runtime-block overlap message, got: %v", err)
	}
}

func TestConstAssignmentRejected(t *testing.T) {
	source := `const LIMIT = 10

function Start()
    LIMIT = 5
    while true
        wait_vblank()
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err == nil {
		t.Fatal("expected error assigning to a constant")
	}
	if !strings.Contains(err.Error(), "constant") {
		t.Errorf("expected constant-assignment message, got: %v", err)
	}
}

func TestGlobalArrays(t *testing.T) {
	source := `const N = 8

var table: int[8]
var bytes: u8[4]
var sum: int = 0
var i: int = 0

function Start()
    i = 0
    while i < N
        table[i] = i * 10
        i = i + 1

    bytes[0] = 250
    bytes[3] = 7

    sum = table[3] + table[7] + bytes[0] + bytes[3]
    while true
        sum = sum
`
	emu, result := compileAndBoot(t, source, 3000)

	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}

	// table[i] = i*10 for i in 0..7
	for i := 0; i < 8; i++ {
		got := read16(emu, addrs["table"]+uint16(i*2))
		if got != uint16(i*10) {
			t.Errorf("table[%d]: want %d, got %d", i, i*10, got)
		}
	}
	if got := emu.CPU.Mem.Read8(0, addrs["bytes"]); got != 250 {
		t.Errorf("bytes[0]: want 250, got %d", got)
	}
	if got := emu.CPU.Mem.Read8(0, addrs["bytes"]+3); got != 7 {
		t.Errorf("bytes[3]: want 7, got %d", got)
	}
	// sum = 30 + 70 + 250 + 7 = 357
	if got := read16(emu, addrs["sum"]); got != 357 {
		t.Errorf("sum: want 357, got %d", got)
	}
}

func TestArrayConstIndexBoundsChecked(t *testing.T) {
	source := `var table: int[4]

function Start()
    table[4] = 1
    while true
        wait_vblank()
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("expected out-of-bounds error, got: %v", err)
	}
}

func TestFixedPointArithmetic(t *testing.T) {
	source := `const SPEED = 3.5
const HALF_SPEED = SPEED / 2.0

var a: fixed = 3.5
var b: fixed = 2.0
var neg: fixed = 0.0
var product: fixed = 0.0
var negprod: fixed = 0.0
var sum: fixed = 0.0
var asint: int = 0
var backtofixed: fixed = 0.0
var rtmul: int = 0
var i: int = 7
var j: int = 9

function Start()
    product = a * b
    neg = 0.0 - 1.5
    negprod = neg * b
    sum = a + HALF_SPEED
    asint = int(product)
    backtofixed = fixed(asint)
    rtmul = i * j
    while true
        wait_vblank()
`
	emu, result := compileAndBoot(t, source, 8000)

	addrs := map[string]uint16{}
	for _, e := range result.MemoryMap {
		addrs[e.Name] = e.Address
	}

	// 3.5 * 2.0 = 7.0 => 0x0700
	if got := read16(emu, addrs["product"]); got != 0x0700 {
		t.Errorf("product 3.5*2.0: want 0x0700 (7.0), got 0x%04X", got)
	}
	// -1.5 * 2.0 = -3.0 => 0xFD00 (two's complement)
	if got := read16(emu, addrs["negprod"]); got != 0xFD00 {
		t.Errorf("negprod -1.5*2.0: want 0xFD00 (-3.0), got 0x%04X", got)
	}
	// 3.5 + 1.75 = 5.25 => 0x0540
	if got := read16(emu, addrs["sum"]); got != 0x0540 {
		t.Errorf("sum 3.5+1.75: want 0x0540 (5.25), got 0x%04X", got)
	}
	// int(7.0) = 7
	if got := read16(emu, addrs["asint"]); got != 7 {
		t.Errorf("int(7.0): want 7, got %d", got)
	}
	// fixed(7) = 0x0700
	if got := read16(emu, addrs["backtofixed"]); got != 0x0700 {
		t.Errorf("fixed(7): want 0x0700, got 0x%04X", got)
	}
	// runtime int multiply 7*9 = 63 (software __mul16)
	if got := read16(emu, addrs["rtmul"]); got != 63 {
		t.Errorf("runtime 7*9: want 63, got %d", got)
	}
}

func TestFixedIntMixRejected(t *testing.T) {
	source := `var speed: fixed = 1.5
var count: int = 3
var out: fixed = 0.0

function Start()
    out = speed * count
    while true
        wait_vblank()
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := CompileProject(srcPath, &CompileOptions{OutputPath: filepath.Join(dir, "main.rom")})
	if err == nil || !strings.Contains(err.Error(), "mix fixed and int") {
		t.Fatalf("expected fixed/int mix error, got: %v", err)
	}
}

// compileLoadForTest compiles source and loads it into a fresh emulator
// without stepping (caller drives Start()/RunFrame for frame-level tests).
func compileLoadForTest(t *testing.T, source string) (*emulator.Emulator, *CompileResult) {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.corelx")
	romPath := filepath.Join(dir, "main.rom")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := CompileProject(srcPath, &CompileOptions{OutputPath: romPath})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	romData, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatalf("read ROM: %v", err)
	}
	emu := emulator.NewEmulator()
	if err := emu.LoadROM(romData); err != nil {
		t.Fatalf("load ROM: %v", err)
	}
	return emu, result
}
