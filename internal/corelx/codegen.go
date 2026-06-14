package corelx

import (
	"errors"
	"fmt"
	"strings"

	"nitro-core-dx/internal/rom"
)

// CodeGenerator generates Nitro Core DX machine code from AST
type CodeGenerator struct {
	program          *Program
	builder          *rom.ROMBuilder
	symbols          map[string]*Symbol
	regAlloc         *RegisterAllocator
	labelCounter     int
	assets           map[string]*AssetDecl
	assetIDs         map[string]uint16
	normalizedAssets map[string]AssetIR
	assetOffsets     map[string]uint16

	// Variable storage tracking
	variables   map[string]*VariableInfo
	varCounter  int
	stackOffset uint16 // Current stack offset for spilled variables

	// Top-level constants and WRAM globals (charter D3).
	consts     map[string]int64
	constFixed map[string]bool
	globals    map[string]*VariableInfo
	memoryMap  []MemoryMapEntry

	// Arithmetic helper routines emitted once after user functions when
	// referenced (signed 8.8 multiply needs 32-bit-correct partials).
	needFixmul bool

	// Function call support
	functionAddrs map[string]int // function name -> code word index of function start
	callPatches   []callPatch    // pending CALL offset patches
	globalStack   uint16         // tracks per-function stack base to avoid overlap
}

// callPatch records a pending CALL that needs its offset patched once the
// target function address is known.
type callPatch struct {
	offsetPos int    // word index of the offset immediate
	target    string // target function name
}

const (
	// Top of WRAM stack region used by CPU Push16/Pop16.
	stackTopAddr = uint16(0x1FFF)
	// Reserve top 256 bytes for CALL/RET return stack frames.
	callStackReserveBytes = uint16(0x0100)
	// Compiler-managed locals/params must stay above this floor.
	stackMinAddr = uint16(0x0100)

	// WRAM global region (charter memory model):
	// 0x2000-0x20FF reserved runtime block (compiler/runtime internal state),
	// 0x2100+       auto-allocated globals,
	// 0x7000-0x7FFF user scratch (never compiler-allocated; mem.* safe zone).
	runtimeBlockBase = uint16(0x2000)
	globalsBase      = uint16(0x2100)
	globalsLimit     = uint16(0x6FFF)
	userScratchBase  = uint16(0x7000)
	userScratchTop   = uint16(0x7FFF)
	// Initial per-function reservation window before spill growth adjustment.
	functionStackWindow = uint16(0x0100)
)

// VariableInfo tracks where a variable is stored
type VariableInfo struct {
	Name       string
	Location   VariableLocation
	RegIndex   uint8  // If in register
	StackAddr  uint16 // If on stack
	StructType string // "Sprite"/"Vec2" when variable stores pointer to a known struct
	ElemWidth  uint8  // element width in bytes for array globals (1 or 2)
	ArrayLen   int    // 0 = scalar; N>0 = fixed-size array
	VarType    string // declared/inferred storage type name ("int","u8","u16","fixed"); "" = unknown
}

// VariableLocation indicates where variable is stored
type VariableLocation int

const (
	VarLocationRegister VariableLocation = iota
	VarLocationStack
	VarLocationMemory
)

type structMemberInfo struct {
	Offset uint16
	Width  uint8 // in bytes (1 or 2)
}

func structTypeNameFromTypeExpr(t TypeExpr) string {
	switch tt := t.(type) {
	case *NamedType:
		if tt.Name == "Sprite" || tt.Name == "Vec2" {
			return tt.Name
		}
	case *PointerType:
		if base, ok := tt.Base.(*NamedType); ok {
			if base.Name == "Sprite" || base.Name == "Vec2" {
				return base.Name
			}
		}
	}
	return ""
}

// RegisterAllocator manages register allocation
type RegisterAllocator struct {
	registers [8]bool  // R0-R7 usage
	spill     []string // Spilled variables
}

var errUnknownBuiltin = errors.New("unknown builtin")

// NewCodeGenerator creates a new code generator
func NewCodeGenerator(program *Program, builder *rom.ROMBuilder) *CodeGenerator {
	return &CodeGenerator{
		program:          program,
		builder:          builder,
		symbols:          make(map[string]*Symbol),
		regAlloc:         &RegisterAllocator{},
		labelCounter:     0,
		assets:           make(map[string]*AssetDecl),
		assetIDs:         make(map[string]uint16),
		normalizedAssets: make(map[string]AssetIR),
		assetOffsets:     make(map[string]uint16),
		variables:        make(map[string]*VariableInfo),
		varCounter:       0,
		stackOffset:      stackTopAddr - callStackReserveBytes, // Below CALL stack reserve
		functionAddrs:    make(map[string]int),
		callPatches:      nil,
		globalStack:      stackTopAddr - callStackReserveBytes, // Reserve top bytes for CALL/RET stack
		consts:           make(map[string]int64),
		constFixed:       make(map[string]bool),
		globals:          make(map[string]*VariableInfo),
	}
}

// MemoryMapEntry records one allocated WRAM symbol for the build's memory
// map listing (name -> address), emitted alongside the ROM for the debugger.
type MemoryMapEntry struct {
	Name    string
	Address uint16
	Size    uint16
	Kind    string // "global" | "global(pin)" | "runtime"
}

// MemoryMap returns the WRAM allocation listing for this build.
func (cg *CodeGenerator) MemoryMap() []MemoryMapEntry { return cg.memoryMap }

// globalTypeSize returns the byte size for a global's declared type.
func globalTypeSize(typeName string) (uint16, error) {
	switch typeName {
	case "u8":
		return 1, nil
	case "int", "i16", "u16", "fixed":
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported global type %q (supported: int, u8, u16, fixed)", typeName)
	}
}

// allocateGlobals folds constants, assigns WRAM addresses to globals
// (auto-allocated from globalsBase; 'at' pins validated for overlap), and
// records the memory map.
func (cg *CodeGenerator) allocateGlobals() error {
	consts, constFixed, err := foldProgramConstsTyped(cg.program)
	if err != nil {
		return err
	}
	cg.consts = consts
	cg.constFixed = constFixed
	// Predefined button-mask constants for the input builtins (bit position
	// per internal/input button layout: UP=0..Z=11).
	for name, mask := range buttonMasks {
		if _, used := cg.consts[name]; !used {
			cg.consts[name] = mask
		}
	}

	cg.memoryMap = append(cg.memoryMap, MemoryMapEntry{
		Name: "__runtime", Address: runtimeBlockBase, Size: globalsBase - runtimeBlockBase, Kind: "runtime",
	})

	type span struct {
		lo, hi uint16
		name   string
	}
	pinned := []span{}
	cursor := globalsBase

	for _, g := range cg.program.Globals {
		if _, dup := cg.globals[g.Name]; dup {
			return fmt.Errorf("line %d: duplicate global %q", g.Position.Line, g.Name)
		}
		if _, isConst := cg.consts[g.Name]; isConst {
			return fmt.Errorf("line %d: global %q collides with a constant of the same name", g.Position.Line, g.Name)
		}
		elemSize, err := globalTypeSize(g.TypeName)
		if err != nil {
			return fmt.Errorf("line %d: global %s: %w", g.Position.Line, g.Name, err)
		}
		size := elemSize
		if g.ArrayLen > 0 {
			size = elemSize * uint16(g.ArrayLen)
			if g.Init != nil {
				return fmt.Errorf("line %d: global array %s cannot take a scalar initializer (arrays zero-initialize)", g.Position.Line, g.Name)
			}
		}

		var addr uint16
		kind := "global"
		if g.HasPin {
			kind = "global(pin)"
			addr = g.PinAddr
			lo, hi := addr, addr+size-1
			if hi >= 0x8000 {
				return fmt.Errorf("line %d: global %s pinned at 0x%04X overlaps the I/O region (>= 0x8000)", g.Position.Line, g.Name, addr)
			}
			if lo >= runtimeBlockBase && lo < globalsBase {
				return fmt.Errorf("line %d: global %s pinned at 0x%04X overlaps the reserved runtime block (0x%04X-0x%04X)", g.Position.Line, g.Name, addr, runtimeBlockBase, globalsBase-1)
			}
			for _, p := range pinned {
				if lo <= p.hi && hi >= p.lo {
					return fmt.Errorf("line %d: global %s pinned at 0x%04X overlaps pinned global %s", g.Position.Line, g.Name, addr, p.name)
				}
			}
			pinned = append(pinned, span{lo, hi, g.Name})
		} else {
			if size == 2 && cursor%2 != 0 {
				cursor++
			}
			addr = cursor
			if uint32(addr)+uint32(size)-1 > uint32(globalsLimit) {
				return fmt.Errorf("global WRAM region exhausted allocating %s (cursor 0x%04X, limit 0x%04X)", g.Name, addr, globalsLimit)
			}
			cursor += size
		}

		cg.globals[g.Name] = &VariableInfo{
			Name:      g.Name,
			Location:  VarLocationStack, // absolute WRAM addressing, same paths as stack slots
			StackAddr: addr,
			ElemWidth: uint8(elemSize),
			ArrayLen:  g.ArrayLen,
			VarType:   g.TypeName,
		}
		cg.memoryMap = append(cg.memoryMap, MemoryMapEntry{Name: g.Name, Address: addr, Size: size, Kind: kind})
	}

	// Auto-allocated globals must not creep into pinned spans.
	for _, p := range pinned {
		if p.lo >= globalsBase && p.lo < cursor {
			return fmt.Errorf("pinned global %s at 0x%04X overlaps the auto-allocated global region (0x%04X-0x%04X); pin it outside that range", p.name, p.lo, globalsBase, cursor-1)
		}
	}
	return nil
}

// emitGlobalInits stores each global's initializer at program entry.
func (cg *CodeGenerator) emitGlobalInits() error {
	for _, g := range cg.program.Globals {
		if g.Init == nil {
			continue
		}
		info := cg.globals[g.Name]
		if v, err := evalConstExpr(g.Init, cg.consts); err == nil {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0)) // MOV R0, #value
			cg.builder.AddImmediate(uint16(v))
		} else {
			if err := cg.generateExpr(g.Init, 0); err != nil {
				return fmt.Errorf("global %s initializer: %w", g.Name, err)
			}
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #addr
		cg.builder.AddImmediate(info.StackAddr)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // MOV [R7], R0
	}
	return nil
}

// SetNormalizedAssets injects compiler-normalized assets so codegen can avoid re-parsing source asset text.
func (cg *CodeGenerator) SetNormalizedAssets(assets []AssetIR) {
	for _, a := range assets {
		cg.normalizedAssets[a.Name] = a
	}
}

// Generate generates code for the program
func (cg *CodeGenerator) Generate() error {
	// Collect assets
	for i, asset := range cg.program.Assets {
		cg.assets[asset.Name] = asset
		cg.assetIDs[asset.Name] = uint16(i)
	}

	// Register symbols
	for _, fn := range cg.program.Functions {
		cg.symbols[fn.Name] = &Symbol{
			Name:   fn.Name,
			IsFunc: true,
		}
	}

	// Generate code for each function
	// Prioritize __Boot() or Start() as the first function (entry point at 0x8000)
	functions := make([]*FunctionDecl, 0, len(cg.program.Functions))
	var entryFunction *FunctionDecl

	// Find entry point function
	for _, fn := range cg.program.Functions {
		if fn.Name == "__Boot" {
			entryFunction = fn
			break
		}
	}
	if entryFunction == nil {
		for _, fn := range cg.program.Functions {
			if fn.Name == "Start" {
				entryFunction = fn
				break
			}
		}
	}

	if err := cg.allocateGlobals(); err != nil {
		return err
	}

	// Add entry function first, then others
	if entryFunction != nil {
		functions = append(functions, entryFunction)
	}
	for _, fn := range cg.program.Functions {
		if fn != entryFunction {
			functions = append(functions, fn)
		}
	}

	// Generate code for each function, recording start addresses.
	for _, fn := range functions {
		cg.functionAddrs[fn.Name] = cg.builder.GetCodeLength()
		if err := cg.generateFunction(fn); err != nil {
			return err
		}
	}

	// Emit arithmetic helper routines (referenced via CALL patches).
	if cg.needFixmul {
		cg.emitFixmulHelper()
	}

	// Patch all pending CALL offsets now that every function address is known.
	for _, patch := range cg.callPatches {
		targetWordIdx, ok := cg.functionAddrs[patch.target]
		if !ok {
			return fmt.Errorf("undefined function: %s", patch.target)
		}
		currentPC := uint16(patch.offsetPos * 2)
		targetPC := uint16(targetWordIdx * 2)
		offset := rom.CalculateBranchOffset(currentPC, targetPC)
		cg.builder.SetImmediateAt(patch.offsetPos, uint16(offset))
	}

	return nil
}

func (cg *CodeGenerator) generateFunction(fn *FunctionDecl) error {
	// Reset variable tracking for each function.
	cg.variables = make(map[string]*VariableInfo)
	cg.regAlloc = &RegisterAllocator{}

	// Each function gets its own non-overlapping stack region (256 bytes).
	// Entry function starts at 0x1FFF, each additional function is 256 bytes lower.
	if cg.globalStack < stackMinAddr+2 {
		return fmt.Errorf("stack allocation exhausted before function %s (global stack base 0x%04X)", fn.Name, cg.globalStack)
	}
	cg.stackOffset = cg.globalStack
	startStack := cg.globalStack
	if cg.globalStack < stackMinAddr+functionStackWindow {
		return fmt.Errorf("stack allocation exhausted reserving frame for function %s (base 0x%04X)", fn.Name, cg.globalStack)
	}
	cg.globalStack -= functionStackWindow

	// Function prologue: save parameters from registers to local stack variables.
	for i, param := range fn.Params {
		if i >= 8 {
			return fmt.Errorf("function %s: too many parameters (max 8)", fn.Name)
		}
		stackAddr, err := cg.allocateStack(2, "function parameter "+param.Name)
		if err != nil {
			return fmt.Errorf("function %s: %w", fn.Name, err)
		}
		// Save R{i} to stack
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(stackAddr)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, uint8(i)))
		paramVarType := ""
		if named, ok := param.Type.(*NamedType); ok {
			paramVarType = named.Name
		}
		cg.variables[param.Name] = &VariableInfo{
			Name:       param.Name,
			Location:   VarLocationStack,
			StackAddr:  stackAddr,
			StructType: structTypeNameFromTypeExpr(param.Type),
			VarType:    paramVarType,
		}
	}

	// The entry function is generated first; its preamble initializes globals.
	if len(cg.functionAddrs) == 1 {
		if err := cg.emitGlobalInits(); err != nil {
			return err
		}
	}

	// Generate function body
	for _, stmt := range fn.Body {
		if err := cg.generateStmt(stmt); err != nil {
			return err
		}
	}

	// Function epilogue
	cg.builder.AddInstruction(rom.EncodeRET())

	// Record how much stack this function used so the next function doesn't overlap.
	used := startStack - cg.stackOffset
	if used > functionStackWindow {
		if startStack < used || startStack-used < stackMinAddr {
			return fmt.Errorf("function %s exceeds available stack (used %d bytes from base 0x%04X)", fn.Name, used, startStack)
		}
		cg.globalStack = startStack - used
	}

	return nil
}

func (cg *CodeGenerator) resolveStructMember(varInfo *VariableInfo, member string) (structMemberInfo, bool) {
	spriteMembers := map[string]structMemberInfo{
		"x_lo": {Offset: 0, Width: 1},
		"x_hi": {Offset: 1, Width: 1},
		"y":    {Offset: 2, Width: 1},
		"tile": {Offset: 3, Width: 1},
		"attr": {Offset: 4, Width: 1},
		"ctrl": {Offset: 5, Width: 1},
	}
	vec2Members := map[string]structMemberInfo{
		"x": {Offset: 0, Width: 2},
		"y": {Offset: 2, Width: 2},
	}

	switch varInfo.StructType {
	case "Sprite":
		v, ok := spriteMembers[member]
		return v, ok
	case "Vec2":
		v, ok := vec2Members[member]
		return v, ok
	}

	// Legacy fallback when struct type is unknown in codegen state.
	if v, ok := spriteMembers[member]; ok {
		return v, true
	}
	if v, ok := vec2Members[member]; ok {
		return v, true
	}
	return structMemberInfo{}, false
}

func (cg *CodeGenerator) emitStructMemberStore(varInfo *VariableInfo, member structMemberInfo, valueReg uint8) bool {
	if varInfo.Location == VarLocationRegister {
		// R7 = struct base address.
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, varInfo.RegIndex))
		if member.Width == 2 {
			// 16-bit store to [R7+offset]
			cg.builder.AddInstruction(rom.EncodeMOV(10, 7, valueReg))
			cg.builder.AddImmediate(member.Offset)
			return true
		}

		// 8-bit store path
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(member.Offset)
		cg.builder.AddInstruction(rom.EncodeADD(0, 7, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(7, 7, valueReg))
		return true
	}

	if varInfo.Location == VarLocationStack {
		// R6 = struct base address loaded from stack slot.
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(varInfo.StackAddr)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 7))

		if member.Width == 2 {
			// 16-bit store to [R6+offset]
			cg.builder.AddInstruction(rom.EncodeMOV(10, 6, valueReg))
			cg.builder.AddImmediate(member.Offset)
			return true
		}

		// 8-bit store path
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(member.Offset)
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))
		cg.builder.AddInstruction(rom.EncodeMOV(7, 6, valueReg))
		return true
	}

	return false
}

func (cg *CodeGenerator) emitStructMemberLoad(varInfo *VariableInfo, member structMemberInfo, destReg uint8) bool {
	if varInfo.Location == VarLocationRegister {
		// R7 = struct base address.
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, varInfo.RegIndex))
		if member.Width == 2 {
			// 16-bit load from [R7+offset]
			cg.builder.AddInstruction(rom.EncodeMOV(9, destReg, 7))
			cg.builder.AddImmediate(member.Offset)
			return true
		}

		// 8-bit load path
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(member.Offset)
		cg.builder.AddInstruction(rom.EncodeADD(0, 7, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(6, destReg, 7))
		return true
	}

	if varInfo.Location == VarLocationStack {
		// R6 = struct base address loaded from stack slot.
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(varInfo.StackAddr)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 7))

		if member.Width == 2 {
			// 16-bit load from [R6+offset]
			cg.builder.AddInstruction(rom.EncodeMOV(9, destReg, 6))
			cg.builder.AddImmediate(member.Offset)
			return true
		}

		// 8-bit load path
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(member.Offset)
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))
		cg.builder.AddInstruction(rom.EncodeMOV(6, destReg, 6))
		return true
	}

	return false
}

func (cg *CodeGenerator) generateStmt(stmt Stmt) error {
	switch s := stmt.(type) {
	case *VarDeclStmt:
		return cg.generateVarDecl(s)

	case *AssignStmt:
		return cg.generateAssign(s)

	case *IfStmt:
		return cg.generateIf(s)

	case *WhileStmt:
		return cg.generateWhile(s)

	case *ForStmt:
		return cg.generateFor(s)

	case *ReturnStmt:
		return cg.generateReturn(s)

	case *ExprStmt:
		return cg.generateExpr(s.Expr, 0) // Discard result

	default:
		return fmt.Errorf("unsupported statement type: %T", stmt)
	}
}

func (cg *CodeGenerator) generateVarDecl(stmt *VarDeclStmt) error {
	// Check if initializer is a struct initialization
	if call, ok := stmt.Value.(*CallExpr); ok {
		if ident, ok := call.Func.(*IdentExpr); ok {
			knownStructs := map[string]bool{"Sprite": true, "Vec2": true}
			if knownStructs[ident.Name] {
				// Struct initialization - use generateCall to allocate and get address
				// This ensures the struct is properly allocated and address is returned
				if err := cg.generateCall(call, 0); err != nil {
					return err
				}
				// R0 now contains the struct address
				// We need to track this address for the variable
				// But we don't know the address at compile time, so we need to store it
				// For now, allocate a register or stack slot to hold the address
				// Actually, we can't store it because we don't know the address until runtime
				// So we need to track that this variable holds a struct address
				// The address is computed at runtime by generateCall
				// We'll need to store R0 somewhere and track it
				// Pre-alpha simplification: keep long-lived locals on stack because
				// builtins use R0-R7 freely and there is no caller/callee-save contract yet.
				stackAddr, err := cg.allocateStack(2, "struct variable "+stmt.Name)
				if err != nil {
					return err
				}
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #stackAddr
				cg.builder.AddImmediate(stackAddr)
				cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // MOV [R7], R0
				cg.variables[stmt.Name] = &VariableInfo{
					Name:       stmt.Name,
					Location:   VarLocationStack,
					StackAddr:  stackAddr,
					StructType: ident.Name,
				}
				return nil
			}
		}
	}

	// Regular variable initialization
	// Generate code for initializer
	if err := cg.generateExpr(stmt.Value, 0); err != nil {
		return err
	}
	// Value is now in R0
	// Pre-alpha simplification: store locals on stack. This avoids register clobbering
	// by builtins until a real calling convention/register allocator is implemented.
	stackAddr, err := cg.allocateStack(2, "variable "+stmt.Name) // Allocate 2 bytes (16-bit value)
	if err != nil {
		return err
	}
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #stackAddr
	cg.builder.AddImmediate(stackAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // MOV [R7], R0
	cg.variables[stmt.Name] = &VariableInfo{
		Name:       stmt.Name,
		Location:   VarLocationStack,
		StackAddr:  stackAddr,
		StructType: structTypeNameFromTypeExpr(stmt.Type),
		VarType:    cg.typeOf(stmt.Value),
	}
	return nil
}

func (cg *CodeGenerator) generateAssign(stmt *AssignStmt) error {
	// Charter D4: assigning across fixed/int without conversion is an error.
	tt, vt := cg.typeOf(stmt.Target), cg.typeOf(stmt.Value)
	if (tt == typeFixed && vt == typeInt) || (tt == typeInt && vt == typeFixed) {
		return fmt.Errorf("cannot assign %s value to %s variable — convert explicitly with int(x) or fixed(x)", vt, tt)
	}
	// Generate code for value
	if err := cg.generateExpr(stmt.Value, 0); err != nil {
		return err
	}
	// Value is in R0
	// Generate code to store to target
	if idx, ok := stmt.Target.(*IndexExpr); ok {
		info, err := cg.resolveArrayGlobal(idx)
		if err != nil {
			return err
		}
		// Preserve R0 (value) in the runtime-block scratch slot while the
		// index expression is evaluated (it may contain calls that clobber
		// registers).
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #scratch
		cg.builder.AddImmediate(runtimeBlockBase)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // MOV [R7], R0
		if err := cg.emitIndexAddr(idx, info); err != nil {
			return err
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #scratch
		cg.builder.AddImmediate(runtimeBlockBase)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 6)) // MOV R5, [R6] (reload value)
		if info.ElemWidth == 2 {
			cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 5)) // 16-bit store [R7], R5
		} else {
			cg.builder.AddInstruction(rom.EncodeMOV(7, 7, 5)) // 8-bit store [R7], R5
		}
		return nil
	}
	if member, ok := stmt.Target.(*MemberExpr); ok {
		// Struct member assignment like hero.tile = base
		if ident, ok := member.Object.(*IdentExpr); ok {
			if varInfo, exists := cg.variables[ident.Name]; exists {
				memberInfo, found := cg.resolveStructMember(varInfo, member.Member)
				if found && cg.emitStructMemberStore(varInfo, memberInfo, 0) {
					return nil
				}
			}
		}
		// Fallback: discard (would need proper struct tracking)
		return nil
	}

	// Regular assignment: x = value
	if ident, ok := stmt.Target.(*IdentExpr); ok {
		if _, isConst := cg.consts[ident.Name]; isConst {
			if _, shadowed := cg.variables[ident.Name]; !shadowed {
				return fmt.Errorf("cannot assign to constant %s", ident.Name)
			}
		}
		if _, exists := cg.variables[ident.Name]; !exists {
			if gInfo, isGlobal := cg.globals[ident.Name]; isGlobal {
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #addr
				cg.builder.AddImmediate(gInfo.StackAddr)
				cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // MOV [R7], R0
				return nil
			}
		}
		if varInfo, exists := cg.variables[ident.Name]; exists {
			// Store value to variable location
			if varInfo.Location == VarLocationRegister {
				cg.builder.AddInstruction(rom.EncodeMOV(0, varInfo.RegIndex, 0)) // MOV R{reg}, R0
			} else if varInfo.Location == VarLocationStack {
				// Store to stack
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #stackAddr
				cg.builder.AddImmediate(varInfo.StackAddr)
				cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // MOV [R7], R0
			}
			return nil
		}
		// Variable not found - this is an error (should have been declared)
		// But for compatibility, create it as a new variable
		// This handles cases like: x = 10 where x wasn't declared
		return cg.generateVarDecl(&VarDeclStmt{
			Position: stmt.Position,
			Name:     ident.Name,
			Value:    stmt.Value,
		})
	}

	return fmt.Errorf("assignment target not supported: %T", stmt.Target)
}

func (cg *CodeGenerator) generateIf(stmt *IfStmt) error {
	// Generate condition
	if err := cg.generateExpr(stmt.Condition, 0); err != nil {
		return err
	}

	// Compare with 0
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #0
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 7)) // CMP R0, R7

	// Branch if false
	elseLabel := cg.newLabel()
	cg.builder.AddInstruction(rom.EncodeBEQ()) // BEQ else_label
	elseOffsetPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0) // Placeholder

	// Generate then block
	for _, s := range stmt.Then {
		if err := cg.generateStmt(s); err != nil {
			return err
		}
	}

	// Jump past else
	endLabel := cg.newLabel()
	cg.builder.AddInstruction(rom.EncodeJMP())
	endOffsetPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0) // Placeholder

	// Generate else block
	cg.patchLabel(elseLabel, elseOffsetPos)
	for _, clause := range stmt.ElseIf {
		// Generate elseif condition
		if err := cg.generateExpr(clause.Condition, 0); err != nil {
			return err
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 7))
		elseIfEnd := cg.newLabel()
		cg.builder.AddInstruction(rom.EncodeBEQ())
		elseIfOffsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		for _, s := range clause.Body {
			if err := cg.generateStmt(s); err != nil {
				return err
			}
		}

		cg.builder.AddInstruction(rom.EncodeJMP())
		elseIfEndOffsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		cg.patchLabel(elseIfEnd, elseIfEndOffsetPos)
		cg.patchLabel(elseIfEnd, elseIfOffsetPos)
	}

	for _, s := range stmt.Else {
		if err := cg.generateStmt(s); err != nil {
			return err
		}
	}

	cg.patchLabel(endLabel, endOffsetPos)
	return nil
}

func (cg *CodeGenerator) generateWhile(stmt *WhileStmt) error {
	loopStartPos := cg.builder.GetCodeLength()

	// Generate condition
	if err := cg.generateExpr(stmt.Condition, 0); err != nil {
		return err
	}

	// Compare with 0
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 7))

	// Branch if false (exit loop)
	loopEnd := cg.newLabel()
	cg.builder.AddInstruction(rom.EncodeBEQ())
	loopEndOffsetPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0) // Placeholder

	// Generate body
	for _, s := range stmt.Body {
		if err := cg.generateStmt(s); err != nil {
			return err
		}
	}

	// Jump back to start
	cg.builder.AddInstruction(rom.EncodeJMP())
	currentPC := uint16(cg.builder.GetCodeLength() * 2)
	offset := rom.CalculateBranchOffset(currentPC, uint16(loopStartPos*2))
	cg.builder.AddImmediate(uint16(offset))

	// Patch loop end
	cg.patchLabel(loopEnd, loopEndOffsetPos)
	return nil
}

func (cg *CodeGenerator) generateFor(stmt *ForStmt) error {
	// BASIC counting loop: for i = start to end [step n], inclusive bounds.
	// Step must be a compile-time constant so the loop direction (and thus the
	// exit comparison) is known at compile time. Default step is +1.
	step := int64(1)
	if stmt.Step != nil {
		v, err := evalConstExpr(stmt.Step, cg.consts)
		if err != nil {
			return fmt.Errorf("for loop 'step' must be a constant: %w", err)
		}
		if v == 0 {
			return fmt.Errorf("for loop 'step' cannot be zero")
		}
		step = v
	}

	// Loop variable: a fresh local in WRAM (same storage as other locals).
	stackAddr, err := cg.allocateStack(2, "for loop variable "+stmt.VarName)
	if err != nil {
		return err
	}
	cg.variables[stmt.VarName] = &VariableInfo{
		Name:      stmt.VarName,
		Location:  VarLocationStack,
		StackAddr: stackAddr,
		VarType:   typeInt,
	}

	// i = start
	if err := cg.generateExpr(stmt.Start, 0); err != nil {
		return err
	}
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(stackAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // [i] = R0

	loopStartPos := cg.builder.GetCodeLength()

	// Re-evaluate the limit each iteration into R1 (correct if it's a variable).
	if err := cg.generateExpr(stmt.End, 1); err != nil {
		return err
	}
	// Load i into R0.
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(stackAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(2, 0, 7)) // R0 = [i]
	// Compare i (R0) to end (R1).
	cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 1))

	// Exit when past the inclusive limit: step>0 exits if i>end (BGT);
	// step<0 exits if i<end (BLT).
	loopEnd := cg.newLabel()
	if step > 0 {
		cg.builder.AddInstruction(rom.EncodeBGT())
	} else {
		cg.builder.AddInstruction(rom.EncodeBLT())
	}
	loopEndOffsetPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0) // placeholder

	// Body.
	for _, st := range stmt.Body {
		if err := cg.generateStmt(st); err != nil {
			return err
		}
	}

	// i = i + step
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(stackAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(2, 0, 7)) // R0 = [i]
	cg.builder.AddInstruction(rom.EncodeMOV(1, 1, 0)) // R1 = #step (as 16-bit two's complement)
	cg.builder.AddImmediate(uint16(int16(step)))
	cg.builder.AddInstruction(rom.EncodeADD(0, 0, 1)) // R0 += step
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(stackAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0)) // [i] = R0

	// Jump back to the condition.
	cg.builder.AddInstruction(rom.EncodeJMP())
	currentPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(currentPC, uint16(loopStartPos*2))))

	cg.patchLabel(loopEnd, loopEndOffsetPos)
	return nil
}

func (cg *CodeGenerator) generateReturn(stmt *ReturnStmt) error {
	if stmt.Value != nil {
		if err := cg.generateExpr(stmt.Value, 0); err != nil {
			return err
		}
		// Value is in R0
	}
	cg.builder.AddInstruction(rom.EncodeRET())
	return nil
}

func (cg *CodeGenerator) generateExpr(expr Expr, destReg uint8) error {
	switch e := expr.(type) {
	case *NumberExpr:
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		value := uint16(e.Value)
		if e.Value > 0xFFFF {
			value = uint16(e.Value & 0xFFFF)
		}
		cg.builder.AddImmediate(value)
		return nil

	case *BoolExpr:
		value := uint16(0)
		if e.Value {
			value = 1
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(value)
		return nil

	case *IdentExpr:
		// Handle built-in constants and variables
		if strings.HasPrefix(e.Name, "ASSET_") {
			// Asset constant
			assetName := strings.TrimPrefix(e.Name, "ASSET_")
			if _, ok := cg.assets[assetName]; ok {
				assetID, ok := cg.assetIDs[assetName]
				if !ok {
					return fmt.Errorf("missing asset ID for %s", assetName)
				}
				// Return compiler-assigned asset ID.
				cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
				cg.builder.AddImmediate(assetID)
				return nil
			}
		}
		// Variable access
		if varInfo, exists := cg.variables[e.Name]; exists {
			// Load from variable location
			if varInfo.Location == VarLocationRegister {
				cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, varInfo.RegIndex)) // MOV R{destReg}, R{reg}
			} else if varInfo.Location == VarLocationStack {
				// Load from stack
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #stackAddr
				cg.builder.AddImmediate(varInfo.StackAddr)
				cg.builder.AddInstruction(rom.EncodeMOV(2, destReg, 7)) // MOV R{destReg}, [R7]
			}
			return nil
		}
		// Compile-time constant: load as immediate.
		if v, isConst := cg.consts[e.Name]; isConst {
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(uint16(v))
			return nil
		}
		// WRAM global: absolute load.
		if gInfo, isGlobal := cg.globals[e.Name]; isGlobal {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #addr
			cg.builder.AddImmediate(gInfo.StackAddr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, destReg, 7)) // MOV R{destReg}, [R7]
			return nil
		}
		// Variable not found - might be a built-in or error
		// For now, return 0 (would be caught in semantic analysis)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0)
		return nil

	case *BinaryExpr:
		// Charter D4: fixed/int operands may not mix without conversion.
		switch e.Op {
		case TOKEN_PLUS, TOKEN_MINUS, TOKEN_STAR, TOKEN_SLASH, TOKEN_PERCENT,
			TOKEN_EQUAL_EQUAL, TOKEN_BANG_EQUAL, TOKEN_LESS, TOKEN_LESS_EQUAL,
			TOKEN_GREATER, TOKEN_GREATER_EQUAL:
			if err := cg.checkNumericMix(tokenOpName(e.Op), e.Left, e.Right); err != nil {
				return err
			}
		}
		// Generate left operand
		if err := cg.generateExpr(e.Left, destReg); err != nil {
			return err
		}
		// Save left result
		cg.builder.AddInstruction(rom.EncodeMOV(0, 1, destReg)) // MOV R1, R{destReg}
		// Generate right operand
		if err := cg.generateExpr(e.Right, 2); err != nil {
			return err
		}
		// Perform operation
		switch e.Op {
		case TOKEN_PLUS:
			// Restore left result to destReg, then add
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // MOV R{destReg}, R1 (restore left)
			cg.builder.AddInstruction(rom.EncodeADD(0, destReg, 2)) // ADD R{destReg}, R2
		case TOKEN_MINUS:
			// Restore left result to destReg, then subtract
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // MOV R{destReg}, R1 (restore left)
			cg.builder.AddInstruction(rom.EncodeSUB(0, destReg, 2)) // SUB R{destReg}, R2
		case TOKEN_STAR:
			// fixed * fixed routes through the signed 8.8 software multiply.
			if cg.typeOf(e.Left) == typeFixed && cg.typeOf(e.Right) == typeFixed {
				cg.needFixmul = true
				cg.builder.AddInstruction(rom.EncodeMOV(0, 0, 1)) // MOV R0, R1 (left)
				cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 2)) // MOV R1, R2 (right)
				cg.emitHelperCall("__fixmul")
				if destReg != 0 {
					cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0))
				}
				return nil
			}
			// General constant multiplication via shift-add decomposition.
			// For runtime multipliers, fall back to shift-add loop.
			if numExpr, ok := e.Right.(*NumberExpr); ok {
				val := numExpr.Value
				if val == 0 {
					cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
					cg.builder.AddImmediate(0)
					return nil
				}
				if val == 1 {
					return nil
				}
				// Shift-add decomposition: decompose val into set bits and
				// generate shifted copies summed together.
				// R1 already has the left operand.
				cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // restore left
				first := true
				for bit := 0; bit < 16; bit++ {
					if val&(1<<uint(bit)) != 0 {
						if bit == 0 {
							if first {
								// destReg already has x
								first = false
							} else {
								cg.builder.AddInstruction(rom.EncodeADD(0, destReg, 1))
							}
						} else {
							// shifted = x << bit
							cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 1))
							cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
							cg.builder.AddImmediate(uint16(bit))
							cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7))
							if first {
								cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 6))
								first = false
							} else {
								cg.builder.AddInstruction(rom.EncodeADD(0, destReg, 6))
							}
						}
					}
				}
				return nil
			}
			// Runtime multiply: hardware MUL (low 16 bits).
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // restore left
			cg.builder.AddInstruction(rom.EncodeMUL(0, destReg, 2)) // MUL R{destReg}, R2
			return nil
		case TOKEN_SLASH:
			if cg.typeOf(e.Left) == typeFixed || cg.typeOf(e.Right) == typeFixed {
				return fmt.Errorf("fixed division is not implemented yet; multiply by a reciprocal constant instead (e.g. x * 0.5)")
			}
			// Hardware DIV (unsigned). Divisor in R2 already; left in R1.
			if numExpr, ok := e.Right.(*NumberExpr); ok && numExpr.Value == 0 {
				return fmt.Errorf("division by zero")
			}
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // restore left
			cg.builder.AddInstruction(rom.EncodeDIV(0, destReg, 2)) // DIV R{destReg}, R2
			return nil
		case TOKEN_PERCENT:
			// General modulo via bitmask (power-of-2) or repeated subtraction.
			if numExpr, ok := e.Right.(*NumberExpr); ok {
				val := numExpr.Value
				if val == 0 {
					return fmt.Errorf("modulo by zero")
				}
				// Power of 2: x % N = x & (N-1)
				if val&(val-1) == 0 {
					cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // restore left
					cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
					cg.builder.AddImmediate(uint16(val - 1))
					cg.builder.AddInstruction(rom.EncodeAND(0, destReg, 7))
					return nil
				}
				// General: repeated subtraction
				cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // restore left
				modStartPos := cg.builder.GetCodeLength()
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
				cg.builder.AddImmediate(uint16(val))
				cg.builder.AddInstruction(rom.EncodeCMP(0, destReg, 7))
				cg.builder.AddInstruction(rom.EncodeBLT())
				modEndPos := cg.builder.GetCodeLength()
				cg.builder.AddImmediate(0)
				cg.builder.AddInstruction(rom.EncodeSUB(0, destReg, 7))
				cg.builder.AddInstruction(rom.EncodeJMP())
				loopPC := uint16(cg.builder.GetCodeLength() * 2)
				cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(loopPC, uint16(modStartPos*2))))
				exitPC := uint16(cg.builder.GetCodeLength() * 2)
				cg.builder.SetImmediateAt(modEndPos, uint16(rom.CalculateBranchOffset(uint16(modEndPos*2), exitPC)))
				return nil
			}
			return fmt.Errorf("runtime modulo not yet implemented (use a constant divisor)")
		case TOKEN_EQUAL_EQUAL:
			// Compare and set result: 1 if equal, 0 if not.
			// Important: branch immediately after CMP (MOV updates flags).
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 2)) // CMP R1, R2
			falseLabel := cg.newLabel()
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeBNE()) // BNE false
			falsePos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // true => 1
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeJMP()) // JMP end
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.patchLabel(falseLabel, falsePos)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // false => 0
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_BANG_EQUAL:
			// Compare and set result: 1 if not equal, 0 if equal.
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 2))
			falseLabel := cg.newLabel()
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeBEQ()) // BEQ false (equal => false)
			falsePos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // true => 1
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.patchLabel(falseLabel, falsePos)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // false => 0
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_LESS:
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 2))
			falseLabel := cg.newLabel()
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeBGE()) // >= => false
			falsePos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // true => 1
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.patchLabel(falseLabel, falsePos)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // false => 0
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_GREATER:
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 2))
			falseLabel := cg.newLabel()
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeBLE()) // <= => false
			falsePos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.patchLabel(falseLabel, falsePos)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_LESS_EQUAL:
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 2))
			falseLabel := cg.newLabel()
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeBGT()) // > => false
			falsePos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.patchLabel(falseLabel, falsePos)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_GREATER_EQUAL:
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 2))
			falseLabel := cg.newLabel()
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeBLT()) // < => false
			falsePos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.patchLabel(falseLabel, falsePos)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_AND:
			// Logical AND: both must be non-zero
			// R1 already has left, R2 has right
			// Set R0 to 1 if both non-zero, else 0
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #0
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 7)) // CMP R1, R7
			cg.builder.AddInstruction(rom.EncodeBEQ())        // BEQ false
			falseLabel1 := cg.newLabel()
			falsePos1 := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 2, 7)) // CMP R2, R7
			cg.builder.AddInstruction(rom.EncodeBEQ())        // BEQ false
			falseLabel2 := cg.newLabel()
			falsePos2 := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			// Both non-zero, set to 1
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(1)
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			// False case
			cg.patchLabel(falseLabel1, falsePos1)
			cg.patchLabel(falseLabel2, falsePos2)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(0)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_OR:
			// Logical OR: at least one non-zero
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 7))
			cg.builder.AddInstruction(rom.EncodeBNE()) // BNE true
			trueLabel1 := cg.newLabel()
			truePos1 := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 2, 7))
			cg.builder.AddInstruction(rom.EncodeBNE()) // BNE true
			trueLabel2 := cg.newLabel()
			truePos2 := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			// Both zero, set to 0
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(0)
			endLabel := cg.newLabel()
			cg.builder.AddInstruction(rom.EncodeJMP())
			endPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			// True case
			cg.patchLabel(trueLabel1, truePos1)
			cg.patchLabel(trueLabel2, truePos2)
			cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
			cg.builder.AddImmediate(1)
			cg.patchLabel(endLabel, endPos)
			return nil
		case TOKEN_PIPE:
			// Bitwise OR: left result is in R1, right result is in R2
			// OR R1, R2 -> result in R1, then move to destReg
			cg.builder.AddInstruction(rom.EncodeOR(0, 1, 2))        // OR R1, R2 -> R1 = R1 | R2
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // MOV R{destReg}, R1
			return nil
		case TOKEN_AMPERSAND:
			// Bitwise AND: left result is in R1, right result is in R2
			// AND R1, R2 -> result in R1, then move to destReg
			cg.builder.AddInstruction(rom.EncodeAND(0, 1, 2))       // AND R1, R2 -> R1 = R1 & R2
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // MOV R{destReg}, R1
			return nil
		case TOKEN_CARET:
			// Bitwise XOR
			cg.builder.AddInstruction(rom.EncodeXOR(0, destReg, 2))
			return nil
		case TOKEN_LSHIFT:
			// Left shift
			cg.builder.AddInstruction(rom.EncodeSHL(0, destReg, 2))
			return nil
		case TOKEN_RSHIFT:
			// Right shift
			cg.builder.AddInstruction(rom.EncodeSHR(0, destReg, 2))
			return nil
		case TOKEN_EQUAL:
			// Assignment operator in expression context (shouldn't happen in binary expr)
			// But handle it anyway
			return fmt.Errorf("assignment operator not allowed in expression context")
		default:
			return fmt.Errorf("binary operator not yet implemented: %v (%d)", e.Op, int(e.Op))
		}
		return nil

	case *CallExpr:
		return cg.generateCall(e, destReg)

	case *MemberExpr:
		// Handle member expressions
		// First check if it's a struct member access (variable exists)
		if ident, ok := e.Object.(*IdentExpr); ok {
			// Check if variable exists first (prioritize variable over namespace)
			if varInfo, exists := cg.variables[ident.Name]; exists {
				memberInfo, found := cg.resolveStructMember(varInfo, e.Member)
				if found && cg.emitStructMemberLoad(varInfo, memberInfo, destReg) {
					return nil
				}
				// Variable exists but member not found - return 0
				cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
				cg.builder.AddImmediate(0)
				return nil
			}
			// Variable doesn't exist - this is an error for member access
			// (Namespace calls like ppu.enable_display() are handled in generateCall)
		}
		// Fallback: generate object and return placeholder
		if err := cg.generateExpr(e.Object, destReg); err != nil {
			return err
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0)
		return nil

	case *UnaryExpr:
		return cg.generateUnary(e, destReg)

	case *IndexExpr:
		return cg.generateIndexLoad(e, destReg)

	case *StringExpr:
		return fmt.Errorf("strings can only be used directly as a text.draw argument in v1 (strings are labels, not a data type)")

	default:
		return fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// resolveArrayGlobal returns the VariableInfo for an index expression whose
// base names a global array.
func (cg *CodeGenerator) resolveArrayGlobal(e *IndexExpr) (*VariableInfo, error) {
	ident, ok := e.Array.(*IdentExpr)
	if !ok {
		return nil, fmt.Errorf("indexing is only supported on named global arrays")
	}
	if _, shadowed := cg.variables[ident.Name]; shadowed {
		return nil, fmt.Errorf("%s is a local variable; indexing is only supported on global arrays", ident.Name)
	}
	info, isGlobal := cg.globals[ident.Name]
	if !isGlobal || info.ArrayLen == 0 {
		return nil, fmt.Errorf("%s is not a global array", ident.Name)
	}
	return info, nil
}

// emitIndexAddr leaves the element's absolute WRAM address in R7.
// Constant indices are bounds-checked at compile time and folded.
func (cg *CodeGenerator) emitIndexAddr(e *IndexExpr, info *VariableInfo) error {
	if v, err := evalConstExpr(e.Index, cg.consts); err == nil {
		if v < 0 || int(v) >= info.ArrayLen {
			return fmt.Errorf("index %d out of bounds for %s[%d]", v, info.Name, info.ArrayLen)
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(info.StackAddr + uint16(v)*uint16(info.ElemWidth))
		return nil
	}
	// Dynamic index: R6 = index, scaled; R7 = base + R6.
	if err := cg.generateExpr(e.Index, 6); err != nil {
		return err
	}
	if info.ElemWidth == 2 {
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 6)) // R6 += R6 (idx*2)
	}
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #base
	cg.builder.AddImmediate(info.StackAddr)
	cg.builder.AddInstruction(rom.EncodeADD(0, 7, 6)) // R7 = base + scaled idx
	return nil
}

// generateIndexLoad loads arr[i] into destReg (8- or 16-bit by element width).
func (cg *CodeGenerator) generateIndexLoad(e *IndexExpr, destReg uint8) error {
	info, err := cg.resolveArrayGlobal(e)
	if err != nil {
		return err
	}
	if err := cg.emitIndexAddr(e, info); err != nil {
		return err
	}
	if info.ElemWidth == 2 {
		cg.builder.AddInstruction(rom.EncodeMOV(2, destReg, 7)) // 16-bit load
	} else {
		cg.builder.AddInstruction(rom.EncodeMOV(6, destReg, 7)) // 8-bit load
	}
	return nil
}

func (cg *CodeGenerator) generateCall(call *CallExpr, destReg uint8) error {
	// Get function name
	var funcName string
	if ident, ok := call.Func.(*IdentExpr); ok {
		funcName = ident.Name
	} else if member, ok := call.Func.(*MemberExpr); ok {
		// Handle member calls like sprite.set_pos
		if obj, ok := member.Object.(*IdentExpr); ok {
			funcName = obj.Name + "." + member.Member
		}
	}

	// Charter D4 numeric conversions.
	if (funcName == "int" || funcName == "fixed") && len(call.Args) == 1 {
		argType := cg.typeOf(call.Args[0])
		if err := cg.generateExpr(call.Args[0], destReg); err != nil {
			return err
		}
		if funcName == "int" && argType != typeInt {
			// fixed -> int: arithmetic shift right 8 (truncates toward -inf).
			cg.builder.AddInstruction(rom.EncodeSHR(3, destReg, 0))
			cg.builder.AddImmediate(8)
		}
		if funcName == "fixed" && argType != typeFixed {
			cg.builder.AddInstruction(rom.EncodeSHL(1, destReg, 0))
			cg.builder.AddImmediate(8)
		}
		return nil
	}

	// text.draw(x, y, r, g, b, "string") streams a string literal to the
	// hardware text port (0x8070-0x8076). Strings are labels in v1 (charter
	// D11), so the literal is emitted inline rather than as string data.
	if funcName == "text.draw" {
		if len(call.Args) != 6 {
			return fmt.Errorf("text.draw expects (x, y, r, g, b, string), got %d args", len(call.Args))
		}
		str, ok := call.Args[5].(*StringExpr)
		if !ok {
			return fmt.Errorf("text.draw: the last argument must be a string literal")
		}
		// X is 16-bit: low byte -> 0x8070, high byte -> 0x8071.
		if err := cg.generateExpr(call.Args[0], 0); err != nil {
			return err
		}
		cg.storeIOByte(0x8070, 0)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 0)) // R6 = X
		cg.builder.AddInstruction(rom.EncodeSHR(1, 6, 0)) // R6 >>= 8
		cg.builder.AddImmediate(8)
		cg.storeIOByte(0x8071, 6)
		// Y, R, G, B are single bytes.
		for i, addr := range []uint16{0x8072, 0x8073, 0x8074, 0x8075} {
			if err := cg.generateExpr(call.Args[i+1], 0); err != nil {
				return err
			}
			cg.storeIOByte(addr, 0)
		}
		// Stream each character to 0x8076 (the port auto-advances X by 8).
		cg.hMovImm(7, 0x8076)
		for _, ch := range str.Value {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0)) // MOV R0, #char
			cg.builder.AddImmediate(uint16(ch))
			cg.builder.AddInstruction(rom.EncodeMOV(7, 7, 0)) // MOV [R7], R0 (8-bit store)
		}
		return nil
	}

	if funcName == "" {
		return fmt.Errorf("cannot determine function name in call")
	}

	// Generate arguments (simplified - pass in R0-R7)
	for i, arg := range call.Args {
		if i >= 8 {
			return fmt.Errorf("too many arguments (max 8)")
		}
		if err := cg.generateExpr(arg, uint8(i)); err != nil {
			return err
		}
	}

	// Try built-in functions first.
	// Propagate real builtin failures instead of masking them as "unknown function".
	if err := cg.generateBuiltinCall(funcName, call.Args, destReg); err == nil {
		return nil
	} else if !errors.Is(err, errUnknownBuiltin) {
		return err
	}

	// Check if it's a user-defined function -- emit CALL instruction.
	if fn := cg.findFunction(funcName); fn != nil {
		// Arguments are already evaluated into R0..R(n-1) by the loop above.
		// Emit CALL with a placeholder offset; patch later.
		cg.builder.AddInstruction(rom.EncodeCALL())
		offsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0) // placeholder
		cg.callPatches = append(cg.callPatches, callPatch{
			offsetPos: offsetPos,
			target:    funcName,
		})
		// Return value is in R0; move to destReg if needed.
		if destReg != 0 {
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0))
		}
		return nil
	}

	// Handle struct initialization like Sprite() or Vec2()
	// Check if it's a known struct type
	knownStructs := map[string]bool{
		"Sprite": true, "Vec2": true,
	}
	if knownStructs[funcName] {
		// Struct initialization creates a zero-initialized struct
		// Allocate stack space for struct (Sprite = 6 bytes)
		structSize := uint16(6) // Sprite struct is 6 bytes
		if funcName == "Vec2" {
			structSize = 4 // Vec2 is 2 i16s = 4 bytes
		}
		stackAddr, err := cg.allocateStack(structSize, "struct "+funcName)
		if err != nil {
			return err
		}

		// Initialize struct to zero
		// Zero out struct memory in 16-bit chunks (current built-ins are even-sized).
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #stackAddr
		cg.builder.AddImmediate(stackAddr)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0
		cg.builder.AddImmediate(0)
		for off := uint16(0); off < structSize; off += 2 {
			cg.builder.AddInstruction(rom.EncodeMOV(10, 7, 6)) // MOV [R7+off], R6 (16-bit store)
			cg.builder.AddImmediate(off)
		}

		// Return struct address in destReg
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(stackAddr)

		// Note: The caller (VarDecl) will track this variable
		// Struct address is returned in destReg
		return nil
	}

	return fmt.Errorf("unknown function: %s", funcName)
}

func (cg *CodeGenerator) findFunction(name string) *FunctionDecl {
	for _, fn := range cg.program.Functions {
		if fn.Name == name {
			return fn
		}
	}
	return nil
}

func (cg *CodeGenerator) generateBuiltinCall(name string, args []Expr, destReg uint8) error {
	switch name {
	case "wait_vblank":
		// Wait for VBlank flag (0x803E, bit 0 = 1 means VBlank)
		// Pattern from manual: Read flag, AND with 0x01, CMP with 0, BEQ if 0 (keep waiting)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x803E
		cg.builder.AddImmediate(0x803E)
		waitPos := cg.builder.GetCodeLength()
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // MOV R5, [R4] (read flag)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #0x01
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeAND(0, 5, 7)) // AND R5, R7 (mask to bit 0)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #0
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 5, 7)) // CMP R5, R7 (compare with 0)
		cg.builder.AddInstruction(rom.EncodeBEQ())        // BEQ waitPos (if equal to 0, keep waiting)
		currentPC := uint16(cg.builder.GetCodeLength() * 2)
		offset := rom.CalculateBranchOffset(currentPC, uint16(waitPos*2))
		cg.builder.AddImmediate(uint16(offset))
		return nil

	case "frame_counter":
		// frame_counter() -> u32 (returns 16-bit frame counter)
		// Read FRAME_COUNTER_LOW (0x803F) and FRAME_COUNTER_HIGH (0x8040)
		// Combine into 16-bit value: (high << 8) | low

		// Read low byte from 0x803F
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x803F
		cg.builder.AddImmediate(0x803F)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // MOV R5, [R4] (read low byte)

		// Read high byte from 0x8040
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8040
		cg.builder.AddImmediate(0x8040)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 4)) // MOV R6, [R4] (read high byte)

		// Combine: (high << 8) | low
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 6)) // MOV R7, R6 (copy high byte)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #8
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 4)) // SHL R7, R4 -> R7 = high << 8
		cg.builder.AddInstruction(rom.EncodeOR(0, 5, 7))  // OR R5, R7 -> R5 = (high << 8) | low

		// Return value in destReg
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 5)) // MOV R{destReg}, R5
		return nil

	case "sprite.set_pos":
		// sprite.set_pos(s: *Sprite, x: i16, y: u8)
		// Args: R0 = sprite pointer, R1 = x (i16), R2 = y (u8)
		// Store x and y to sprite struct
		// R0 has sprite address (from &hero), R1 has x, R2 has y
		// Write x_lo (low byte of x)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 0)) // MOV R3, R0 (save sprite addr)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 1)) // MOV R4, R1 (save x)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 2)) // MOV R5, R2 (save y)

		// Write x_lo (offset 0) - low byte of x
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0xFF
		cg.builder.AddImmediate(0xFF)
		cg.builder.AddInstruction(rom.EncodeAND(0, 4, 6)) // AND R4, R6 (mask to low byte)
		cg.builder.AddInstruction(rom.EncodeMOV(7, 3, 4)) // MOV [R3], R4 (8-bit store x_lo)

		// Write x_hi (offset 1) - high byte (sign bit)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 1)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 4)) // MOV R6, R4 (copy x)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #8
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHR(0, 6, 7)) // SHR R6, R7 -> R6 = x >> 8 (high byte)
		cg.builder.AddInstruction(rom.EncodeMOV(7, 3, 6)) // MOV [R3], R6 (write x_hi)
		// Write y (offset 2)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 2)
		cg.builder.AddInstruction(rom.EncodeMOV(7, 3, 5)) // MOV [R3], R5 (write y)
		return nil

	case "oam.write":
		// oam.write(id: u8, s: *Sprite)
		// Args: R0 = sprite id, R1 = sprite pointer
		// Set OAM_ADDR to id * 6, then write sprite data from struct to OAM_DATA

		// Save sprite pointer (R1) to R3
		cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 1)) // MOV R3, R1 (sprite pointer)

		// Calculate OAM address: id * 6
		cg.builder.AddInstruction(rom.EncodeMOV(0, 2, 0)) // MOV R2, R0 (save id)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 2)) // MOV R6, R2 (copy id)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7)) // SHL R6, R7 -> R6 = id*2
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 2)) // MOV R7, R2 (id)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #2
		cg.builder.AddImmediate(2)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 5)) // SHL R7, R5 -> R7 = id*4
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7)) // ADD R6, R7 -> R6 = id*2 + id*4 = id*6

		// Set OAM_ADDR (0x8014)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8014
		cg.builder.AddImmediate(0x8014)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 6)) // MOV R5, R6 (OAM offset)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (write OAM_ADDR)

		// Set OAM_DATA address (0x8015)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8015
		cg.builder.AddImmediate(0x8015)

		// Read sprite struct and write to OAM_DATA
		// Sprite format: x_lo (offset 0), x_hi (offset 1), y (offset 2), tile (offset 3), attr (offset 4), ctrl (offset 5)
		// R3 = sprite pointer (save original in R7 for later use if needed)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 3)) // MOV R7, R3 (save original sprite pointer)

		// Write x_lo (offset 0)
		cg.builder.AddInstruction(rom.EncodeMOV(6, 5, 3)) // MOV R5, [R3] (8-bit load, mode 6)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (write to OAM_DATA)

		// Write x_hi (offset 1)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 1)
		cg.builder.AddInstruction(rom.EncodeMOV(6, 5, 3)) // MOV R5, [R3]
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

		// Write y (offset 2)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 2)
		cg.builder.AddInstruction(rom.EncodeMOV(6, 5, 3)) // MOV R5, [R3]
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

		// Write tile (offset 3)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 3)
		cg.builder.AddInstruction(rom.EncodeMOV(6, 5, 3)) // MOV R5, [R3]
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

		// Write attr (offset 4)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 4)
		cg.builder.AddInstruction(rom.EncodeMOV(6, 5, 3)) // MOV R5, [R3]
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

		// Write ctrl (offset 5)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 6)) // ADD R3, R6 (increment to offset 5)
		cg.builder.AddInstruction(rom.EncodeMOV(6, 5, 3)) // MOV R5, [R3]
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

		return nil

	case "oam.write_sprite_data":
		// oam.write_sprite_data(id: u8, x: i16, y: u8, tile: u8, attr: u8, ctrl: u8)
		// Args: R0=id, R1=x, R2=y, R3=tile, R4=attr, R5=ctrl
		// Preserve y in R6 and keep attr/ctrl in R4/R5 by using R0/R2/R7 as temporaries.
		if len(args) != 6 {
			return fmt.Errorf("oam.write_sprite_data requires 6 arguments")
		}

		idReg := uint8(0)
		xReg := uint8(1)
		yReg := uint8(2)
		tileReg := uint8(3)
		attrReg := uint8(4)
		ctrlReg := uint8(5)
		// Save y (R2) because R2 will be reused for x temp values
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, yReg)) // MOV R6, R2 (save y)

		// Set OAM_ADDR to sprite ID (0-127), NOT id * 6
		// The PPU internally multiplies by 6 to get the byte offset
		// Write sprite ID directly to OAM_ADDR (0x8014)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 2, 0)) // MOV R2, #0x8014
		cg.builder.AddImmediate(0x8014)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 2, idReg)) // MOV [R2], R0 (sprite ID)

		// Write sprite data to OAM_DATA (0x8015) using R0 as pointer (id no longer needed)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0)) // MOV R0, #0x8015
		cg.builder.AddImmediate(0x8015)

		// X low byte (R1)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 2, xReg)) // MOV R2, R1 (x temp)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))    // MOV R7, #0xFF
		cg.builder.AddImmediate(0xFF)
		cg.builder.AddInstruction(rom.EncodeAND(0, 2, 7)) // AND R2, R7 (low byte)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 0, 2)) // MOV [R0], R2

		// X high byte: extract sign bit from x (bit 8)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 2, xReg)) // MOV R2, R1 (x)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))    // MOV R7, #8
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHR(0, 2, 7)) // SHR R2, R7 -> x high
		cg.builder.AddInstruction(rom.EncodeMOV(3, 0, 2)) // MOV [R0], R2

		// Y (from saved R6)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 0, 6)) // MOV [R0], R6

		// Tile (R3)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 0, tileReg)) // MOV [R0], R3

		// Attr (R4)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 0, attrReg)) // MOV [R0], R4

		// Ctrl (R5)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 0, ctrlReg)) // MOV [R0], R5
		return nil

	case "oam.clear_sprite":
		// oam.clear_sprite(id: u8)
		// Args: R0 = sprite id
		// Disables sprite by setting control byte to 0
		if len(args) != 1 {
			return fmt.Errorf("oam.clear_sprite requires 1 argument")
		}

		idReg := uint8(0)

		// Set OAM_ADDR to sprite ID, then write 0 to control byte (byte 5)
		// Write sprite ID to OAM_ADDR (0x8014)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8014
		cg.builder.AddImmediate(0x8014)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, idReg)) // MOV R6, R0 (sprite ID)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 6))     // MOV [R4], R6 (write sprite ID to OAM_ADDR)

		// Set OAM_ADDR to sprite ID again, but this time we need to write to byte 5 (control)
		// The PPU uses OAM_ADDR * 6 + byte_index, so we need to write 5 dummy bytes first
		// Actually, simpler: just write 0 to all 6 bytes to completely disable the sprite
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8015
		cg.builder.AddImmediate(0x8015)
		// Write 0 to all 6 bytes (X_low, X_high, Y, Tile, Attr, Ctrl)
		for i := 0; i < 6; i++ {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 6)) // MOV [R4], R6
		}
		return nil

	case "SPR_PAL":
		// SPR_PAL(p: u8) -> u8
		// Returns palette value (p & 0x0F)
		if len(args) != 1 {
			return fmt.Errorf("SPR_PAL requires 1 argument")
		}
		// Arg is in R0, mask to 4 bits
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #0x0F
		cg.builder.AddImmediate(0x0F)
		cg.builder.AddInstruction(rom.EncodeAND(0, destReg, 7)) // AND R{destReg}, R7
		return nil

	case "SPR_PRI":
		// SPR_PRI(p: u8) -> u8
		// Returns priority value shifted to bits [7:6] of attr byte
		// Priority is in bits [7:6] of byte 4 (Attributes)
		// Shift priority value left by 6 bits: p << 6
		if len(args) != 1 {
			return fmt.Errorf("SPR_PRI requires 1 argument")
		}
		// Arg is in R0, shift left by 6 bits
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0)) // MOV R{destReg}, R0
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))       // MOV R7, #6
		cg.builder.AddImmediate(6)
		cg.builder.AddInstruction(rom.EncodeSHL(0, destReg, 7)) // SHL R{destReg}, R7 -> priority << 6
		return nil

	case "SPR_HFLIP":
		// SPR_HFLIP() -> u8
		// Returns 0x10 (horizontal flip bit, bit 4 of attr byte)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0x10)
		return nil

	case "SPR_VFLIP":
		// SPR_VFLIP() -> u8
		// Returns 0x20 (vertical flip bit, bit 5 of attr byte)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0x20)
		return nil

	case "SPR_SIZE_8":
		// SPR_SIZE_8() -> u8
		// Returns 0x00 (8×8 size, bit 1 of ctrl byte = 0)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0x00)
		return nil

	case "SPR_ENABLE":
		// SPR_ENABLE() -> u8
		// Returns 0x01 (enable bit, bit 0 of ctrl byte)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0x01)
		return nil

	case "SPR_SIZE_16":
		// SPR_SIZE_16() -> u8
		// Returns 0x02 (16×16 size bit, bit 1 of ctrl byte = 1)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
		cg.builder.AddImmediate(0x02)
		return nil

	case "SPR_BLEND":
		// SPR_BLEND(mode: u8) -> u8
		// Returns blend mode shifted to bits [3:2] of ctrl byte
		// Blend mode is in bits [3:2] of byte 5 (Control)
		// Shift mode left by 2 bits: mode << 2
		if len(args) != 1 {
			return fmt.Errorf("SPR_BLEND requires 1 argument")
		}
		// Arg is in R0, shift left by 2 bits
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0)) // MOV R{destReg}, R0
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))       // MOV R7, #2
		cg.builder.AddImmediate(2)
		cg.builder.AddInstruction(rom.EncodeSHL(0, destReg, 7)) // SHL R{destReg}, R7 -> mode << 2
		return nil

	case "SPR_ALPHA":
		// SPR_ALPHA(a: u8) -> u8
		// Returns alpha value shifted to bits [7:4] of ctrl byte
		// Alpha is in bits [7:4] of byte 5 (Control)
		// Shift alpha left by 4 bits: a << 4
		if len(args) != 1 {
			return fmt.Errorf("SPR_ALPHA requires 1 argument")
		}
		// Arg is in R0, mask to 4 bits first (alpha is 0-15), then shift left by 4
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0)) // MOV R{destReg}, R0
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))       // MOV R7, #0x0F
		cg.builder.AddImmediate(0x0F)
		cg.builder.AddInstruction(rom.EncodeAND(0, destReg, 7)) // AND R{destReg}, R7 -> mask to 4 bits
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))       // MOV R7, #4
		cg.builder.AddImmediate(4)
		cg.builder.AddInstruction(rom.EncodeSHL(0, destReg, 7)) // SHL R{destReg}, R7 -> alpha << 4
		return nil

	case "oam.flush":
		// oam.flush() - no-op for now
		return nil

	case "gfx.set_palette":
		// gfx.set_palette(palette: u8, color_index: u8, color: u16)
		// Args: R0 = palette (0-15), R1 = color_index (0-15), R2 = color (RGB555, 16-bit)
		// Sets a color in CGRAM
		// CGRAM address = (palette * 16 + color_index) * 2
		// CGRAM is RGB555 format, stored as 2 bytes (low, high)

		// Calculate CGRAM color index address: (palette * 16 + color_index)
		// Note: PPU CGRAM_ADDR register is in color-index units (the PPU multiplies by 2 internally
		// when writing low/high bytes into CGRAM storage), so we must NOT multiply by 2 here.
		// palette * 16 = palette << 4
		cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 0)) // MOV R3, R0 (save palette)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #4
		cg.builder.AddImmediate(4)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 3, 4)) // SHL R3, R4 -> R3 = palette << 4 = palette * 16
		cg.builder.AddInstruction(rom.EncodeADD(0, 3, 1)) // ADD R3, R1 -> R3 = palette * 16 + color_index

		// Set CGRAM_ADDR (0x8012)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0x8012
		cg.builder.AddImmediate(0x8012)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 3)) // MOV R7, R3 (CGRAM color index address)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0xFF
		cg.builder.AddImmediate(0xFF)
		cg.builder.AddInstruction(rom.EncodeAND(0, 7, 5)) // AND R7, R5 (mask to 8 bits for CGRAM_ADDR)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7)) // MOV [R6], R7 (write CGRAM_ADDR)

		// Write color to CGRAM_DATA (0x8013)
		// CGRAM_DATA requires two writes: low byte, then high byte (both to same address)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0x8013
		cg.builder.AddImmediate(0x8013)

		// Write low byte first
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 2)) // MOV R7, R2 (color)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0xFF
		cg.builder.AddImmediate(0xFF)
		cg.builder.AddInstruction(rom.EncodeAND(0, 7, 5)) // AND R7, R5 (mask to low byte)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7)) // MOV [R6], R7 (write low byte)

		// Write high byte (triggers CGRAM write)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 2)) // MOV R7, R2 (color)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #8
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHR(0, 7, 5)) // SHR R7, R5 -> R7 = color >> 8 (high byte)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7)) // MOV [R6], R7 (write high byte, triggers write)
		return nil

	case "gfx.init_default_palettes":
		// gfx.init_default_palettes()
		// Initializes default palettes with basic colors
		// Palette 0: Grayscale (black to white)
		// Palette 1: Blue tones
		// Palette 2: Green tones
		// Palette 3: Red tones

		// Initialize palette 0 (grayscale)
		for i := 0; i < 16; i++ {
			// Color value: RGB555, grayscale = (i*31/15, i*31/15, i*31/15)
			// Simplified: use i*2 for each component (0-30 range)
			comp := uint16(i * 2)
			if comp > 31 {
				comp = 31
			}
			// RGB555: RRRRR GGGGG BBBBB (bits 15-11=R, 10-6=G, 5-1=B, bit 0 unused)
			color := (comp << 11) | (comp << 6) | (comp << 1)

			// Set palette 0, color i
			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8012 (CGRAM_ADDR)
			cg.builder.AddImmediate(0x8012)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #(i*2)
			cg.builder.AddImmediate(uint16(i * 2))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

			// Write color
			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8013 (CGRAM_DATA)
			cg.builder.AddImmediate(0x8013)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_low
			cg.builder.AddImmediate(color & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (low byte)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_high
			cg.builder.AddImmediate((color >> 8) & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (high byte)
		}

		// Initialize palette 1 (blue tones) - simplified, just set a few colors
		// Color 0 = black, Color 15 = bright blue
		for i := 0; i < 16; i++ {
			comp := uint16(i * 2)
			if comp > 31 {
				comp = 31
			}
			// Blue: (0, 0, comp)
			color := (comp << 1) // Blue in bits 5-1

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8012
			cg.builder.AddImmediate(0x8012)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #(16*2 + i*2)
			cg.builder.AddImmediate(uint16(16*2 + i*2))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8013
			cg.builder.AddImmediate(0x8013)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_low
			cg.builder.AddImmediate(color & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_high
			cg.builder.AddImmediate((color >> 8) & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
		}

		// Initialize palette 2 (green tones)
		for i := 0; i < 16; i++ {
			comp := uint16(i * 2)
			if comp > 31 {
				comp = 31
			}
			// Green: (0, comp, 0)
			color := (comp << 6) // Green in bits 10-6

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8012
			cg.builder.AddImmediate(0x8012)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #(32*2 + i*2)
			cg.builder.AddImmediate(uint16(32*2 + i*2))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8013
			cg.builder.AddImmediate(0x8013)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_low
			cg.builder.AddImmediate(color & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_high
			cg.builder.AddImmediate((color >> 8) & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
		}

		// Initialize palette 3 (red tones)
		for i := 0; i < 16; i++ {
			comp := uint16(i * 2)
			if comp > 31 {
				comp = 31
			}
			// Red: (comp, 0, 0)
			color := (comp << 11) // Red in bits 15-11

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8012
			cg.builder.AddImmediate(0x8012)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #(48*2 + i*2)
			cg.builder.AddImmediate(uint16(48*2 + i*2))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8013
			cg.builder.AddImmediate(0x8013)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_low
			cg.builder.AddImmediate(color & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #color_high
			cg.builder.AddImmediate((color >> 8) & 0xFF)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
		}

		return nil

	case "ppu.enable_display":
		// Enable display (BG0_CONTROL = 0x8008, bit 0 = enable)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8008
		cg.builder.AddImmediate(0x8008)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0x01
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5
		return nil

	case "gfx.load_tiles":
		// gfx.load_tiles(asset: u16, base: u16) -> u16
		// Args: R0 = asset ID (ASSET_* constant), R1 = base tile index
		// Loads tile data from asset to VRAM starting at base * 32 bytes
		// Returns base tile index (for chaining)

		// Check if first arg is an ASSET_ constant (compile-time known)
		if len(args) > 0 {
			if ident, ok := args[0].(*IdentExpr); ok && strings.HasPrefix(ident.Name, "ASSET_") {
				assetName := strings.TrimPrefix(ident.Name, "ASSET_")
				if asset, exists := cg.assets[assetName]; exists {
					// We know the asset at compile time - inline the data writes
					return cg.generateInlineTileLoad(asset, args[1], destReg)
				}
			}
		}

		if len(cg.program.Assets) == 0 {
			return fmt.Errorf("gfx.load_tiles requires declared assets in this source file")
		}
		return cg.generateRuntimeTileLoadDispatch(destReg)

	case "input.read":
		// input.read() -> u16
		// Read controller 1 buttons (16-bit)
		// Latch buttons first, then read
		// Latch: write 1 to 0xA001
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0xA001
		cg.builder.AddImmediate(0xA001)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (latch)
		// Read low byte from 0xA000
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0xA000
		cg.builder.AddImmediate(0xA000)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // MOV R5, [R4] (read low byte)
		// Read high byte from 0xA001
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0xA001
		cg.builder.AddImmediate(0xA001)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 4)) // MOV R6, [R4] (read high byte)
		// Combine: R5 (low) | (R6 << 8) (high)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #8
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7)) // SHL R6, R7 -> R6 = high << 8
		cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))  // OR R5, R6 -> R5 = low | (high << 8)
		// Release latch: write 0 to 0xA001
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0xA001
		cg.builder.AddImmediate(0xA001)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 6)) // MOV [R4], R6 (release latch)
		// Return value in destReg
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 5)) // MOV R{destReg}, R5
		return nil

	// APU Functions
	case "apu.enable":
		// apu.enable() - Enable APU master volume
		// Write 0xFF to MASTER_VOLUME (0x9020)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x9020
		cg.builder.AddImmediate(0x9020)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0xFF
		cg.builder.AddImmediate(0xFF)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (write master volume)
		return nil

	case "apu.set_channel_wave":
		// apu.set_channel_wave(ch: u8, wave: u8)
		// Args: R0 = channel (0-3), R1 = waveform (0-3)
		// Write to CONTROL register (offset +3) with bits [1:2] = waveform
		// Channel base: CH0=0x9000, CH1=0x9008, CH2=0x9010, CH3=0x9018
		// CONTROL = channel_base + 3

		// Calculate channel base address: 0x9000 + (ch * 8)
		// ch * 8 = ch << 3
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (save channel)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5)) // SHL R4, R5 -> R4 = ch << 3 = ch * 8
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0x9000
		cg.builder.AddImmediate(0x9000)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = 0x9000 + (ch * 8)

		// Add offset 3 for CONTROL register
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = channel_base + 3

		// Prepare waveform value: shift to bits [1:2]
		// Waveform is in R1, need to shift left by 1 bit
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1)) // MOV R5, R1 (waveform)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 5, 6)) // SHL R5, R6 -> R5 = wave << 1

		// Read current CONTROL value, OR with waveform bits, write back
		// For simplicity, just write waveform bits (assumes enable bit will be set separately)
		// In practice, we'd read, mask, OR, write - but for now just write waveform
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (write CONTROL)
		return nil

	case "apu.set_channel_freq":
		// apu.set_channel_freq(ch: u8, freq: u16)
		// Args: R0 = channel (0-3), R1 = frequency (16-bit)
		// Write low byte to FREQ_LOW (offset +0), then high byte to FREQ_HIGH (offset +1)
		// Writing high byte triggers phase reset

		// Calculate channel base address: 0x9000 + (ch * 8)
		// ch * 8 = ch << 3
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (save channel)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5)) // SHL R4, R5 -> R4 = ch << 3 = ch * 8
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0x9000
		cg.builder.AddImmediate(0x9000)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = 0x9000 + (ch * 8)

		// Save frequency value
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1)) // MOV R5, R1 (frequency)

		// Write low byte to FREQ_LOW (offset +0)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 5)) // MOV R6, R5 (copy freq)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #0xFF
		cg.builder.AddImmediate(0xFF)
		cg.builder.AddInstruction(rom.EncodeAND(0, 6, 7)) // AND R6, R7 -> R6 = freq & 0xFF (low byte)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 6)) // MOV [R4], R6 (write FREQ_LOW)

		// Write high byte to FREQ_HIGH (offset +1)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #1
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 6)) // ADD R4, R6 -> R4 = channel_base + 1
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 5)) // MOV R6, R5 (copy freq)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0)) // MOV R7, #8
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHR(0, 6, 7)) // SHR R6, R7 -> R6 = freq >> 8 (high byte)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 6)) // MOV [R4], R6 (write FREQ_HIGH, triggers phase reset)
		return nil

	case "apu.set_channel_volume":
		// apu.set_channel_volume(ch: u8, vol: u8)
		// Args: R0 = channel (0-3), R1 = volume (0-255)
		// Write to VOLUME register (offset +2)

		// Calculate channel base address: 0x9000 + (ch * 8)
		// ch * 8 = ch << 3
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (save channel)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5)) // SHL R4, R5 -> R4 = ch << 3 = ch * 8
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0x9000
		cg.builder.AddImmediate(0x9000)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = 0x9000 + (ch * 8)

		// Add offset 2 for VOLUME register
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #2
		cg.builder.AddImmediate(2)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = channel_base + 2

		// Write volume (R1) to VOLUME register
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 1)) // MOV [R4], R1 (write VOLUME)
		return nil

	case "apu.note_on":
		// apu.note_on(ch: u8)
		// Args: R0 = channel (0-3)
		// Set CONTROL register (offset +3) bit 0 to 1 (enable)

		// Calculate channel base address: 0x9000 + (ch * 8)
		// ch * 8 = ch << 3
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (save channel)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5)) // SHL R4, R5 -> R4 = ch << 3 = ch * 8
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0x9000
		cg.builder.AddImmediate(0x9000)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = 0x9000 + (ch * 8)

		// Add offset 3 for CONTROL register
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = channel_base + 3

		// Read current CONTROL value, OR with 0x01 (enable bit)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // MOV R5, [R4] (read current CONTROL)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0x01
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))  // OR R5, R6 -> R5 = CONTROL | 0x01
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (write CONTROL with enable bit)
		return nil

	case "apu.note_off":
		// apu.note_off(ch: u8)
		// Args: R0 = channel (0-3)
		// Clear CONTROL register (offset +3) bit 0 (disable)

		// Calculate channel base address: 0x9000 + (ch * 8)
		// ch * 8 = ch << 3
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (save channel)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5)) // SHL R4, R5 -> R4 = ch << 3 = ch * 8
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #0x9000
		cg.builder.AddImmediate(0x9000)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = 0x9000 + (ch * 8)

		// Add offset 3 for CONTROL register
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #3
		cg.builder.AddImmediate(3)
		cg.builder.AddInstruction(rom.EncodeADD(0, 4, 5)) // ADD R4, R5 -> R4 = channel_base + 3

		// Read current CONTROL value, AND with 0xFE (clear enable bit)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // MOV R5, [R4] (read current CONTROL)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0xFE
		cg.builder.AddImmediate(0xFE)
		cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6)) // AND R5, R6 -> R5 = CONTROL & 0xFE (clear bit 0)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (write CONTROL without enable bit)
		return nil

	case "mem.write":
		// mem.write(addr: u16, value: u8)
		// Args: R0 = address, R1 = value
		// Writes an 8-bit value to any memory-mapped address.
		if len(args) != 2 {
			return fmt.Errorf("mem.write requires 2 arguments (addr, value)")
		}
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (addr)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1)) // MOV R5, R1 (value)
		cg.builder.AddInstruction(rom.EncodeMOV(7, 4, 5)) // MOV [R4], R5 (8-bit store)
		return nil

	case "mem.read":
		// mem.read(addr: u16) -> u8
		// Args: R0 = address
		// Reads an 8-bit value from any memory-mapped address.
		if len(args) != 1 {
			return fmt.Errorf("mem.read requires 1 argument (addr)")
		}
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0))       // MOV R4, R0 (addr)
		cg.builder.AddInstruction(rom.EncodeMOV(6, destReg, 4)) // MOV R{dest}, [R4] (8-bit read)
		return nil

	case "mem.write16":
		// mem.write16(addr: u16, value: u16)
		// 16-bit store. In WRAM this is true little-endian 16-bit; on I/O
		// addresses (bank 0, >= 0x8000) the memory system writes only the low
		// byte (8-bit MMIO) -- the same hardware-accurate routing the FPGA must
		// implement. Args: R0 = address, R1 = value.
		if len(args) != 2 {
			return fmt.Errorf("mem.write16 requires 2 arguments (addr, value)")
		}
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0)) // MOV R4, R0 (addr)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1)) // MOV R5, R1 (value)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // MOV [R4], R5 (16-bit store)
		return nil

	case "input.poll":
		// input.poll() latches and reads the controller, saving the previous
		// frame's state first so input.pressed/released can detect edges. The
		// curr/prev state lives in the compiler-reserved runtime block.
		if len(args) != 0 {
			return fmt.Errorf("input.poll takes no arguments")
		}
		// prev = curr
		cg.hLoad16(0, inputCurrSlot)
		cg.hStore16(inputPrevSlot, 0)
		// latch: write 1 to 0xA001
		cg.hMovImm(4, 0xA001)
		cg.hMovImm(5, 1)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // [0xA001] = 1
		// read low (0xA000) and high (0xA001) bytes, combine
		cg.hMovImm(4, 0xA000)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // R5 = [0xA000] low
		cg.hMovImm(4, 0xA001)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 4)) // R6 = [0xA001] high
		cg.hMovImm(7, 8)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7)) // R6 = high << 8
		cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))  // R5 = low | (high<<8)
		// release latch: write 0 to 0xA001
		cg.hMovImm(4, 0xA001)
		cg.hMovImm(6, 0)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 6)) // [0xA001] = 0
		// curr = combined state
		cg.hStore16(inputCurrSlot, 5)
		return nil

	case "input.held":
		// input.held(button) -> nonzero if the button is down this frame.
		if len(args) != 1 {
			return fmt.Errorf("input.held requires 1 argument (a button, e.g. UP)")
		}
		if err := cg.generateExpr(args[0], 1); err != nil { // R1 = mask
			return err
		}
		cg.hLoad16(0, inputCurrSlot)                       // R0 = curr
		cg.builder.AddInstruction(rom.EncodeAND(0, 0, 1))  // R0 = curr & mask
		if destReg != 0 {
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0))
		}
		return nil

	case "input.pressed":
		// input.pressed(button) -> nonzero on the rising edge (down now, up last
		// frame): (curr & mask) & ~(prev & mask).
		if len(args) != 1 {
			return fmt.Errorf("input.pressed requires 1 argument (a button, e.g. A)")
		}
		if err := cg.generateExpr(args[0], 1); err != nil { // R1 = mask
			return err
		}
		cg.hLoad16(0, inputCurrSlot)
		cg.builder.AddInstruction(rom.EncodeAND(0, 0, 1)) // R0 = curr & mask
		cg.hLoad16(2, inputPrevSlot)
		cg.builder.AddInstruction(rom.EncodeAND(0, 2, 1)) // R2 = prev & mask
		cg.hMovImm(7, 0xFFFF)
		cg.builder.AddInstruction(rom.EncodeXOR(0, 2, 7)) // R2 = ~(prev & mask)
		cg.builder.AddInstruction(rom.EncodeAND(0, 0, 2)) // R0 = (curr&mask) & ~(prev&mask)
		if destReg != 0 {
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0))
		}
		return nil

	case "input.released":
		// input.released(button) -> nonzero on the falling edge:
		// (prev & mask) & ~(curr & mask).
		if len(args) != 1 {
			return fmt.Errorf("input.released requires 1 argument (a button)")
		}
		if err := cg.generateExpr(args[0], 1); err != nil { // R1 = mask
			return err
		}
		cg.hLoad16(0, inputPrevSlot)
		cg.builder.AddInstruction(rom.EncodeAND(0, 0, 1)) // R0 = prev & mask
		cg.hLoad16(2, inputCurrSlot)
		cg.builder.AddInstruction(rom.EncodeAND(0, 2, 1)) // R2 = curr & mask
		cg.hMovImm(7, 0xFFFF)
		cg.builder.AddInstruction(rom.EncodeXOR(0, 2, 7)) // R2 = ~(curr & mask)
		cg.builder.AddInstruction(rom.EncodeAND(0, 0, 2)) // R0 = (prev&mask) & ~(curr&mask)
		if destReg != 0 {
			cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 0))
		}
		return nil

	case "mem.read16":
		// mem.read16(addr: u16) -> u16
		// 16-bit load. In WRAM this is true little-endian 16-bit; on I/O
		// addresses the memory system zero-extends an 8-bit read. Args: R0 = addr.
		if len(args) != 1 {
			return fmt.Errorf("mem.read16 requires 1 argument (addr)")
		}
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 0))       // MOV R4, R0 (addr)
		cg.builder.AddInstruction(rom.EncodeMOV(2, destReg, 4)) // MOV R{dest}, [R4] (16-bit read)
		return nil

	case "bg.set_scroll":
		// bg.set_scroll(layer: u8, scroll_x: i16, scroll_y: i16)
		// Args: R0 = layer (0-3), R1 = scroll_x, R2 = scroll_y
		// BG scroll register layout:
		//   BG0: 0x8000/01 (X), 0x8002/03 (Y)
		//   BG1: 0x8004/05 (X), 0x8006/07 (Y)
		//   BG2: 0x800A/0B (X), 0x800C/0D (Y)
		//   BG3: 0x8022/23 (X), 0x8024/25 (Y)
		// For simplicity, generate a lookup for each layer.
		if len(args) != 3 {
			return fmt.Errorf("bg.set_scroll requires 3 arguments (layer, x, y)")
		}
		// Save args
		cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 0)) // R3 = layer
		cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 1)) // R4 = scroll_x
		cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 2)) // R5 = scroll_y

		// Helper: write scroll_x low and high bytes, then scroll_y low and high bytes.
		// We'll generate inline code for each layer check.
		bgScrollAddrs := [][2]uint16{
			{0x8000, 0x8002}, // BG0 X/Y base
			{0x8004, 0x8006}, // BG1 X/Y base
			{0x800A, 0x800C}, // BG2 X/Y base
			{0x8022, 0x8024}, // BG3 X/Y base
		}
		jumpToEnd := make([]int, 0, 4)
		for i, addrs := range bgScrollAddrs {
			// if R3 != i, skip
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 3, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			// Write scroll_x low byte
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(addrs[0])
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 4))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(0xFF)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

			// Write scroll_x high byte
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(addrs[0] + 1)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 4))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHR(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

			// Write scroll_y low byte
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(addrs[1])
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(0xFF)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

			// Write scroll_y high byte
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(addrs[1] + 1)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHR(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

			// Jump to end
			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			// Patch skip
			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		// Patch all jumps to end
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.enable":
		// bg.enable(layer: u8)
		// Args: R0 = layer (0-3)
		// BG control registers: BG0=0x8008, BG1=0x8009, BG2=0x8021, BG3=0x8026
		// Set bit 0 to enable.
		if len(args) != 1 {
			return fmt.Errorf("bg.enable requires 1 argument (layer)")
		}
		bgCtrlAddrs := []uint16{0x8008, 0x8009, 0x8021, 0x8026}
		jumpToEnd2 := make([]int, 0, 4)
		for i, addr := range bgCtrlAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			// Read current control, OR with 0x01, write back
			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // read
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x01)
			cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // write

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd2 = append(jumpToEnd2, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC2 := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd2 {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC2)))
		}
		return nil

	case "bg.disable":
		// bg.disable(layer: u8)
		// Args: R0 = layer (0-3)
		// BG control registers: BG0=0x8008, BG1=0x8009, BG2=0x8021, BG3=0x8026
		// Clear bit 0 to disable while preserving other control bits.
		if len(args) != 1 {
			return fmt.Errorf("bg.disable requires 1 argument (layer)")
		}
		bgCtrlAddrs := []uint16{0x8008, 0x8009, 0x8021, 0x8026}
		jumpToEnd2 := make([]int, 0, 4)
		for i, addr := range bgCtrlAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // read
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0xFE)
			cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5)) // write

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd2 = append(jumpToEnd2, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC2 := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd2 {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC2)))
		}
		return nil

	case "bg.set_tile_size":
		// bg.set_tile_size(layer: u8, size: u16)
		// Args: R0 = layer (0-3), R1 = size (8 or 16)
		// Bit 1 in BG control selects 16x16 tiles when set.
		if len(args) != 2 {
			return fmt.Errorf("bg.set_tile_size requires 2 arguments (layer, size)")
		}
		bgCtrlAddrs := []uint16{0x8008, 0x8009, 0x8021, 0x8026}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgCtrlAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // current control
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0xFD)                     // clear tile-size bit
			cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6)) // R5 &= 0xFD

			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(16)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			noTileSetPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x02)
			cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))

			noTileSetPC := uint16(cg.builder.GetCodeLength() * 2)
			noTileSetBranchPC := uint16(noTileSetPos * 2)
			cg.builder.SetImmediateAt(noTileSetPos, uint16(rom.CalculateBranchOffset(noTileSetBranchPC, noTileSetPC)))

			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.set_priority":
		// bg.set_priority(layer: u8, priority: u8)
		// Args: R0 = layer, R1 = priority (0-3)
		// BG control registers: BG0=0x8008, BG1=0x8009, BG2=0x8021, BG3=0x8026
		// Bits [3:2] carry explicit layer priority.
		if len(args) != 2 {
			return fmt.Errorf("bg.set_priority requires 2 arguments (layer, priority)")
		}
		bgCtrlAddrs := []uint16{0x8008, 0x8009, 0x8021, 0x8026}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgCtrlAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // R5 = current control
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0xF3)                     // clear bits [3:2]
			cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6)) // R5 = current & 0xF3

			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1)) // R7 = priority
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x03)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 6)) // R7 &= 0x03
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(2)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 6)) // R7 <<= 2
			cg.builder.AddInstruction(rom.EncodeOR(0, 5, 7))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.set_tilemap_base":
		// bg.set_tilemap_base(layer: u8, base: u16)
		// Args: R0 = layer, R1 = tilemap base
		// Register pairs: BG0=0x8077/78, BG1=0x8079/7A, BG2=0x807B/7C, BG3=0x807D/7E
		if len(args) != 2 {
			return fmt.Errorf("bg.set_tilemap_base requires 2 arguments (layer, base)")
		}
		bgTilemapAddrs := []uint16{0x8077, 0x8079, 0x807B, 0x807D}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgTilemapAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0xFF)
			cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr + 1)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHR(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.load_tilemap":
		// bg.load_tilemap(asset: u16, layer: u8) -> u16
		// Args: R0 = asset ID (ASSET_* constant), R1 = layer
		// Loads packed tilemap bytes into the bound layer tilemap base. Returns the
		// tilemap base used for the transfer.
		if len(args) != 2 {
			return fmt.Errorf("bg.load_tilemap requires 2 arguments (asset, layer)")
		}

		if ident, ok := args[0].(*IdentExpr); ok && strings.HasPrefix(ident.Name, "ASSET_") {
			assetName := strings.TrimPrefix(ident.Name, "ASSET_")
			if asset, exists := cg.assets[assetName]; exists {
				return cg.generateInlineTilemapLoad(asset, args[1], destReg)
			}
		}

		if len(cg.program.Assets) == 0 {
			return fmt.Errorf("bg.load_tilemap requires declared tilemap assets in this source file")
		}
		return cg.generateRuntimeTilemapLoadDispatch(destReg)

	case "bg.set_tile":
		// bg.set_tile(layer: u8, x: u16, y: u16, tile: u8, attr: u8)
		// Args: R0 = layer, R1 = x, R2 = y, R3 = tile, R4 = attr
		// Writes a single 2-byte tilemap entry at the active layer tilemap base.
		// If the tilemap base register is unset (0), the helper falls back to 0x4000
		// to match the renderer's default tilemap base.
		if len(args) != 5 {
			return fmt.Errorf("bg.set_tile requires 5 arguments (layer, x, y, tile, attr)")
		}
		bgTilemapAddrs := []uint16{0x8077, 0x8079, 0x807B, 0x807D}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgTilemapAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			// Resolve tilemap base into R5.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 6)) // R5 = low byte
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(addr + 1)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 6)) // R6 = high byte
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 5, 7))
			cg.builder.AddInstruction(rom.EncodeBNE())
			baseReadyPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x4000)
			baseReadyPC := uint16(cg.builder.GetCodeLength() * 2)
			cg.builder.SetImmediateAt(baseReadyPos, uint16(rom.CalculateBranchOffset(uint16(baseReadyPos*2), baseReadyPC)))

			// offset = ((y << 5) + x) << 1
			cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 2))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(5)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeADD(0, 6, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeADD(0, 5, 6))

			// Program VRAM address registers with R5.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x800E)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(0x00FF)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x800F)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHR(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

			// Write tile + attr via auto-incrementing VRAM_DATA.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x8010)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 3))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 4))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.fill_span":
		// bg.fill_span(layer: u8, x: u16, y: u16, count: u16, tile: u8, attr: u8)
		// Args: R0 = layer, R1 = x, R2 = y, R3 = count, R4 = tile, R5 = attr
		// Fills a contiguous run of tilemap entries on a single row.
		if len(args) != 6 {
			return fmt.Errorf("bg.fill_span requires 6 arguments (layer, x, y, count, tile, attr)")
		}
		bgTilemapAddrs := []uint16{0x8077, 0x8079, 0x807B, 0x807D}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgTilemapAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			// Resolve tilemap base into R6.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 7))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(addr + 1)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 7, 7))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeOR(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeBNE())
			baseReadyPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x4000)
			baseReadyPC := uint16(cg.builder.GetCodeLength() * 2)
			cg.builder.SetImmediateAt(baseReadyPos, uint16(rom.CalculateBranchOffset(uint16(baseReadyPos*2), baseReadyPC)))

			// startAddr = base + (((y << 5) + x) << 1)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 2))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(5)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeADD(0, 7, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 0))
			cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))

			// Program VRAM address once; VRAM_DATA auto-increments.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0x800E)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(0x00FF)
			cg.builder.AddInstruction(rom.EncodeAND(0, 1, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 1))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0x800F)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHR(0, 1, 0))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 1))

			// Loop count times writing tile/attr pairs.
			loopStart := cg.builder.GetCodeLength()
			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 3, 0))
			cg.builder.AddInstruction(rom.EncodeBEQ())
			loopEndPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0x8010)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 4))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 5))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeSUB(0, 3, 0))

			cg.builder.AddInstruction(rom.EncodeJMP())
			loopJumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(uint16(loopJumpPos*2), uint16(loopStart*2))))

			loopEndPC := uint16(cg.builder.GetCodeLength() * 2)
			cg.builder.SetImmediateAt(loopEndPos, uint16(rom.CalculateBranchOffset(uint16(loopEndPos*2), loopEndPC)))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.clear":
		// bg.clear(layer: u8, tile: u8, attr: u8)
		// Args: R0 = layer, R1 = tile, R2 = attr
		// Clears the full 32x32 tilemap for a layer.
		if len(args) != 3 {
			return fmt.Errorf("bg.clear requires 3 arguments (layer, tile, attr)")
		}
		bgTilemapAddrs := []uint16{0x8077, 0x8079, 0x807B, 0x807D}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgTilemapAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			// Resolve tilemap base into R4.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 4, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(addr + 1)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeOR(0, 4, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 4, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			baseReadyPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(0x4000)
			baseReadyPC := uint16(cg.builder.GetCodeLength() * 2)
			cg.builder.SetImmediateAt(baseReadyPos, uint16(rom.CalculateBranchOffset(uint16(baseReadyPos*2), baseReadyPC)))

			// Program VRAM address once.
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x800E)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 4))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0x00FF)
			cg.builder.AddInstruction(rom.EncodeAND(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x800F)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 4))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(8)
			cg.builder.AddInstruction(rom.EncodeSHR(0, 6, 7))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
			cg.builder.AddImmediate(1024)

			loopStart := cg.builder.GetCodeLength()
			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(0)
			cg.builder.AddInstruction(rom.EncodeCMP(0, 3, 4))
			cg.builder.AddInstruction(rom.EncodeBEQ())
			loopEndPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(0x8010)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 2))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeSUB(0, 3, 4))

			cg.builder.AddInstruction(rom.EncodeJMP())
			loopJumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(uint16(loopJumpPos*2), uint16(loopStart*2))))

			loopEndPC := uint16(cg.builder.GetCodeLength() * 2)
			cg.builder.SetImmediateAt(loopEndPos, uint16(rom.CalculateBranchOffset(uint16(loopEndPos*2), loopEndPC)))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "matrix_plane.set_projection":
		// set_projection(channel, mode, horizon): selects the plane, then sets
		// projection mode (0x8091) and horizon scanline (0x8092). mode: 0 none,
		// 1 perspective row projection, 2 vertical projected quad.
		if len(args) != 3 {
			return fmt.Errorf("matrix_plane.set_projection requires (channel, mode, horizon)")
		}
		if err := cg.selectMatrixPlane(args[0]); err != nil {
			return err
		}
		if err := cg.writePlaneByte(args[1], 0x8091); err != nil {
			return err
		}
		return cg.writePlaneByte(args[2], 0x8092)

	case "matrix_plane.set_depth":
		// set_depth(channel, base_distance, focal_length, width_scale): the 8.8
		// projection-depth registers (0x809B, 0x809D, 0x809F).
		if len(args) != 4 {
			return fmt.Errorf("matrix_plane.set_depth requires (channel, base_distance, focal_length, width_scale)")
		}
		if err := cg.selectMatrixPlane(args[0]); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[1], 0x809B); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[2], 0x809D); err != nil {
			return err
		}
		return cg.writePlaneReg16(args[3], 0x809F)

	case "matrix_plane.set_camera":
		// set_camera(channel, x, y, heading_x, heading_y): camera position
		// (0x8093/0x8095) and 8.8 heading vector (0x8097/0x8099).
		if len(args) != 5 {
			return fmt.Errorf("matrix_plane.set_camera requires (channel, x, y, heading_x, heading_y)")
		}
		if err := cg.selectMatrixPlane(args[0]); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[1], 0x8093); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[2], 0x8095); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[3], 0x8097); err != nil {
			return err
		}
		return cg.writePlaneReg16(args[4], 0x8099)

	case "matrix_plane.set_surface":
		// set_surface(channel, origin_x, origin_y, facing_x, facing_y, height_scale):
		// world anchor (0x80A1/0x80A3), 8.8 facing vector (0x80A5/0x80A7), and
		// 8.8 vertical size (0x80A9) for vertical-projected-quad surfaces.
		if len(args) != 6 {
			return fmt.Errorf("matrix_plane.set_surface requires (channel, origin_x, origin_y, facing_x, facing_y, height_scale)")
		}
		if err := cg.selectMatrixPlane(args[0]); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[1], 0x80A1); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[2], 0x80A3); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[3], 0x80A5); err != nil {
			return err
		}
		if err := cg.writePlaneReg16(args[4], 0x80A7); err != nil {
			return err
		}
		return cg.writePlaneReg16(args[5], 0x80A9)

	case "matrix_plane.enable":
		// matrix_plane.enable(channel: u8, size: u16)
		// Args: R0 = channel, R1 = size in tiles (32, 64, 128)
		if len(args) != 2 {
			return fmt.Errorf("matrix_plane.enable requires 2 arguments (channel, size)")
		}
		cg.emitSelectMatrixPlane(0, 6, 7)
		// Default to 32x32 enabled.
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0x01)

		// if size == 64 -> control = 0x03
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(64)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 6))
		cg.builder.AddInstruction(rom.EncodeBNE())
		not64Pos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0x03)
		cg.builder.AddInstruction(rom.EncodeJMP())
		after64Pos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		not64PC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(not64Pos, uint16(rom.CalculateBranchOffset(uint16(not64Pos*2), not64PC)))

		// if size == 128 -> control = 0x05
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(128)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 1, 6))
		cg.builder.AddInstruction(rom.EncodeBNE())
		not128Pos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0x05)
		not128PC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(not128Pos, uint16(rom.CalculateBranchOffset(uint16(not128Pos*2), not128PC)))

		after64PC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(after64Pos, uint16(rom.CalculateBranchOffset(uint16(after64Pos*2), after64PC)))

		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x8081)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 5))
		return nil

	case "matrix_plane.disable":
		// matrix_plane.disable(channel: u8)
		// Args: R0 = channel
		if len(args) != 1 {
			return fmt.Errorf("matrix_plane.disable requires 1 argument (channel)")
		}
		cg.emitSelectMatrixPlane(0, 6, 7)
		cg.emitLoadMMIO8(5, 0x8081)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x06)
		cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x8081)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 5))
		return nil

	case "matrix_plane.load_tiles":
		// matrix_plane.load_tiles(asset: u16, channel: u8, base: u16) -> u16
		// Args: R0 = asset ID, R1 = channel, R2 = base tile index
		if len(args) != 3 {
			return fmt.Errorf("matrix_plane.load_tiles requires 3 arguments (asset, channel, base)")
		}
		if ident, ok := args[0].(*IdentExpr); ok && strings.HasPrefix(ident.Name, "ASSET_") {
			assetName := strings.TrimPrefix(ident.Name, "ASSET_")
			if asset, exists := cg.assets[assetName]; exists {
				return cg.generateInlineMatrixPlaneTileLoadFromRegs(asset, 1, 2, destReg)
			}
		}
		if len(cg.program.Assets) == 0 {
			return fmt.Errorf("matrix_plane.load_tiles requires declared tile assets in this source file")
		}
		return cg.generateRuntimeMatrixPlaneTileLoadDispatch(destReg)

	case "matrix_plane.load_tilemap":
		// matrix_plane.load_tilemap(asset: u16, channel: u8)
		// Args: R0 = asset ID, R1 = channel
		if len(args) != 2 {
			return fmt.Errorf("matrix_plane.load_tilemap requires 2 arguments (asset, channel)")
		}
		if ident, ok := args[0].(*IdentExpr); ok && strings.HasPrefix(ident.Name, "ASSET_") {
			assetName := strings.TrimPrefix(ident.Name, "ASSET_")
			if asset, exists := cg.assets[assetName]; exists {
				return cg.generateInlineMatrixPlaneTilemapLoadFromChannelReg(asset, 1, destReg)
			}
		}
		if len(cg.program.Assets) == 0 {
			return fmt.Errorf("matrix_plane.load_tilemap requires declared tilemap assets in this source file")
		}
		return cg.generateRuntimeMatrixPlaneTilemapLoadDispatch(destReg)

	case "matrix_plane.set_tile":
		// matrix_plane.set_tile(channel: u8, x: u16, y: u16, tile: u8, attr: u8)
		// Args: R0 = channel, R1 = x, R2 = y, R3 = tile, R4 = attr
		if len(args) != 5 {
			return fmt.Errorf("matrix_plane.set_tile requires 5 arguments (channel, x, y, tile, attr)")
		}
		cg.emitSelectMatrixPlane(0, 6, 7)
		cg.emitSelectedMatrixPlaneShift(5, 6, 7)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 2)) // R6 = y
		cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 5))
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 1))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(0x8082)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 0, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0x00FF)
		cg.builder.AddInstruction(rom.EncodeAND(0, 0, 5))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(0x8083)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 0, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHR(0, 0, 5))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 0))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(0x8084)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 3))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 7, 4))
		return nil

	case "matrix_plane.fill_rect":
		// matrix_plane.fill_rect(channel: u8, x: u16, y: u16, w: u16, h: u16, tile: u8, attr: u8)
		// Args: R0 = channel, R1 = x, R2 = y, R3 = w, R4 = h, R5 = tile, R6 = attr
		if len(args) != 7 {
			return fmt.Errorf("matrix_plane.fill_rect requires 7 arguments (channel, x, y, w, h, tile, attr)")
		}
		return cg.generateMatrixPlaneFillRect()

	case "matrix_plane.clear":
		// matrix_plane.clear(channel: u8, tile: u8, attr: u8)
		// Args: R0 = channel, R1 = tile, R2 = attr
		if len(args) != 3 {
			return fmt.Errorf("matrix_plane.clear requires 3 arguments (channel, tile, attr)")
		}
		return cg.generateMatrixPlaneClear()

	case "raster.enable":
		// raster.enable(table_base: u16, layer_mask: u8, rebind: bool, priority: bool, tilemap_base: bool, source_mode: bool)
		// Args:
		//   R0 = scanline table base in VRAM
		//   R1 = layer mask bits [3:0] for BG0-BG3
		//   R2 = include rebind table
		//   R3 = include priority table
		//   R4 = include tilemap-base table
		//   R5 = include source-mode table
		if len(args) != 6 {
			return fmt.Errorf("raster.enable requires 6 arguments (table_base, layer_mask, rebind, priority, tilemap_base, source_mode)")
		}

		// Program table base.
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x805E)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 0))
		cg.builder.AddInstruction(rom.EncodeAND(1, 7, 0))
		cg.builder.AddImmediate(0x00FF)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x805F)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 0))
		cg.builder.AddInstruction(rom.EncodeSHR(1, 7, 0))
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

		// Build HDMA control byte in R7.
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(0x01) // enable bit

		// Layer mask -> bits [4:1]
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 1))
		cg.builder.AddInstruction(rom.EncodeAND(1, 6, 0))
		cg.builder.AddImmediate(0x0F)
		cg.builder.AddInstruction(rom.EncodeSHL(1, 6, 0))
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeOR(0, 7, 6))

		// rebind -> bit 5
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 2))
		cg.builder.AddInstruction(rom.EncodeAND(1, 6, 0))
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeSHL(1, 6, 0))
		cg.builder.AddImmediate(5)
		cg.builder.AddInstruction(rom.EncodeOR(0, 7, 6))

		// priority -> bit 6
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 3))
		cg.builder.AddInstruction(rom.EncodeAND(1, 6, 0))
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeSHL(1, 6, 0))
		cg.builder.AddImmediate(6)
		cg.builder.AddInstruction(rom.EncodeOR(0, 7, 6))

		// tilemap_base -> bit 7
		cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 4))
		cg.builder.AddInstruction(rom.EncodeAND(1, 6, 0))
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeSHL(1, 6, 0))
		cg.builder.AddImmediate(7)
		cg.builder.AddInstruction(rom.EncodeOR(0, 7, 6))

		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x805D)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

		// Extension control: source-mode table bit 0.
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(0x807F)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
		cg.builder.AddInstruction(rom.EncodeAND(1, 7, 0))
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))
		return nil

	case "raster.disable":
		// raster.disable()
		// Clears scanline-command enablement and extension flags.
		if len(args) != 0 {
			return fmt.Errorf("raster.disable requires 0 arguments")
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
		cg.builder.AddImmediate(0x805D)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
		cg.builder.AddImmediate(0x807F)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))
		return nil

	case "raster.set_scanline_scroll":
		// raster.set_scanline_scroll(scanline: u16, layer: u8, scroll_x: i16, scroll_y: i16)
		// Args: R0 = scanline, R1 = layer, R2 = scroll_x, R3 = scroll_y
		if len(args) != 4 {
			return fmt.Errorf("raster.set_scanline_scroll requires 4 arguments (scanline, layer, scroll_x, scroll_y)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
		cg.builder.AddInstruction(rom.EncodeSHL(1, 7, 0))
		cg.builder.AddImmediate(4)
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))
		cg.emitWriteVRAM16AtAddrReg(6, 2, 7, 0)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(2)
		cg.emitWriteVRAM16AtAddrReg(6, 3, 7, 0)
		return nil

	case "raster.set_scanline_matrix":
		// raster.set_scanline_matrix(scanline: u16, layer: u8, a: i16, b: i16, c: i16, d: i16)
		// Args: R0 = scanline, R1 = layer, R2 = A, R3 = B, R4 = C, R5 = D
		if len(args) != 6 {
			return fmt.Errorf("raster.set_scanline_matrix requires 6 arguments (scanline, layer, a, b, c, d)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
		cg.builder.AddInstruction(rom.EncodeSHL(1, 7, 0))
		cg.builder.AddImmediate(4)
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(4)
		cg.emitWriteVRAM16AtAddrReg(6, 2, 7, 0)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(2)
		cg.emitWriteVRAM16AtAddrReg(6, 3, 7, 0)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(2)
		cg.emitWriteVRAM16AtAddrReg(6, 4, 7, 0)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(2)
		cg.emitWriteVRAM16AtAddrReg(6, 5, 7, 0)
		return nil

	case "raster.set_scanline_center":
		// raster.set_scanline_center(scanline: u16, layer: u8, center_x: i16, center_y: i16)
		// Args: R0 = scanline, R1 = layer, R2 = center_x, R3 = center_y
		if len(args) != 4 {
			return fmt.Errorf("raster.set_scanline_center requires 4 arguments (scanline, layer, center_x, center_y)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
		cg.builder.AddInstruction(rom.EncodeSHL(1, 7, 0))
		cg.builder.AddImmediate(4)
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(12)
		cg.emitWriteVRAM16AtAddrReg(6, 2, 7, 0)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(2)
		cg.emitWriteVRAM16AtAddrReg(6, 3, 7, 0)
		return nil

	case "raster.set_scanline_tilemap_base":
		// raster.set_scanline_tilemap_base(scanline: u16, layer: u8, tilemap_base: u16)
		// Args: R0 = scanline, R1 = layer, R2 = tilemap base
		if len(args) != 3 {
			return fmt.Errorf("raster.set_scanline_tilemap_base requires 3 arguments (scanline, layer, tilemap_base)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(64)
		cg.emitMaybeAddImmediateIfMMIOFlag(0x805D, 0x20, 4, 6, 7)
		cg.emitMaybeAddImmediateIfMMIOFlag(0x805D, 0x40, 4, 6, 7)

		cg.emitLoadMMIO8(7, 0x805D)
		cg.builder.AddInstruction(rom.EncodeAND(1, 7, 0))
		cg.builder.AddImmediate(0x80)
		cg.builder.AddInstruction(rom.EncodeBEQ())
		skipPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
		cg.builder.AddInstruction(rom.EncodeSHL(1, 7, 0))
		cg.builder.AddImmediate(1)
		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 7))
		cg.emitWriteVRAM16AtAddrReg(6, 2, 7, 0)

		skipPC := uint16(skipPos * 2)
		nextPC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		return nil

	case "raster.set_scanline_rebind":
		// raster.set_scanline_rebind(scanline: u16, layer: u8, channel: u8)
		// Args: R0 = scanline, R1 = layer, R2 = channel
		if len(args) != 3 {
			return fmt.Errorf("raster.set_scanline_rebind requires 3 arguments (scanline, layer, channel)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(64)

		cg.emitLoadMMIO8(7, 0x805D)
		cg.builder.AddInstruction(rom.EncodeAND(1, 7, 0))
		cg.builder.AddImmediate(0x20)
		cg.builder.AddInstruction(rom.EncodeBEQ())
		skipPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 1))
		cg.emitWriteVRAM8AtAddrReg(6, 2, 7, 0)

		skipPC := uint16(skipPos * 2)
		nextPC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		return nil

	case "raster.set_scanline_priority":
		// raster.set_scanline_priority(scanline: u16, layer: u8, priority: u8)
		// Args: R0 = scanline, R1 = layer, R2 = priority
		if len(args) != 3 {
			return fmt.Errorf("raster.set_scanline_priority requires 3 arguments (scanline, layer, priority)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(64)
		cg.emitMaybeAddImmediateIfMMIOFlag(0x805D, 0x20, 4, 6, 7)

		cg.emitLoadMMIO8(7, 0x805D)
		cg.builder.AddInstruction(rom.EncodeAND(1, 7, 0))
		cg.builder.AddImmediate(0x40)
		cg.builder.AddInstruction(rom.EncodeBEQ())
		skipPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 1))
		cg.emitWriteVRAM8AtAddrReg(6, 2, 7, 0)

		skipPC := uint16(skipPos * 2)
		nextPC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		return nil

	case "raster.set_scanline_source_mode":
		// raster.set_scanline_source_mode(scanline: u16, layer: u8, mode: u8)
		// Args: R0 = scanline, R1 = layer, R2 = mode
		if len(args) != 3 {
			return fmt.Errorf("raster.set_scanline_source_mode requires 3 arguments (scanline, layer, mode)")
		}
		cg.emitComputeRasterScanlineBase(0, 6, 7)
		cg.builder.AddInstruction(rom.EncodeADD(1, 6, 0))
		cg.builder.AddImmediate(64)
		cg.emitMaybeAddImmediateIfMMIOFlag(0x805D, 0x20, 4, 6, 7)
		cg.emitMaybeAddImmediateIfMMIOFlag(0x805D, 0x40, 4, 6, 7)
		cg.emitMaybeAddImmediateIfMMIOFlag(0x805D, 0x80, 8, 6, 7)

		cg.emitLoadMMIO8(7, 0x807F)
		cg.builder.AddInstruction(rom.EncodeAND(1, 7, 0))
		cg.builder.AddImmediate(0x01)
		cg.builder.AddInstruction(rom.EncodeBEQ())
		skipPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeADD(0, 6, 1))
		cg.emitWriteVRAM8AtAddrReg(6, 2, 7, 0)

		skipPC := uint16(skipPos * 2)
		nextPC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		return nil

	case "bg.set_source_mode":
		// bg.set_source_mode(layer: u8, mode: u8)
		// Args: R0 = layer, R1 = source mode (0=tilemap, 1=bitmap)
		if len(args) != 2 {
			return fmt.Errorf("bg.set_source_mode requires 2 arguments (layer, mode)")
		}
		bgSourceModeAddrs := []uint16{0x8068, 0x8069, 0x806A, 0x806B}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgSourceModeAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x01)
			cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "bg.bind_transform", "matrix.bind":
		// bg.bind_transform(layer: u8, channel: u8)
		// matrix.bind(layer: u8, channel: u8)
		// Args: R0 = layer, R1 = transform channel (0-3)
		if len(args) != 2 {
			return fmt.Errorf("%s requires 2 arguments (layer, channel)", name)
		}
		bgTransformBindAddrs := []uint16{0x806C, 0x806D, 0x806E, 0x806F}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range bgTransformBindAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 1))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(0x03)
			cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "matrix.enable", "matrix.disable":
		// matrix.enable(layer: u8)
		// matrix.disable(layer: u8)
		// Args: R0 = layer
		if len(args) != 1 {
			return fmt.Errorf("%s requires 1 argument (layer)", name)
		}
		matrixControlAddrs := []uint16{0x8018, 0x802B, 0x8038, 0x8045}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range matrixControlAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 4)) // current control
			if name == "matrix.enable" {
				cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
				cg.builder.AddImmediate(0x01)
				cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))
			} else {
				cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
				cg.builder.AddImmediate(0xFE)
				cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
			}
			cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "matrix.set_matrix":
		// matrix.set_matrix(layer: u8, a: i16, b: i16, c: i16, d: i16)
		// Args: R0 = layer, R1 = A, R2 = B, R3 = C, R4 = D
		if len(args) != 5 {
			return fmt.Errorf("matrix.set_matrix requires 5 arguments (layer, a, b, c, d)")
		}
		matrixAAddrs := []uint16{0x8019, 0x802C, 0x8039, 0x8046}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range matrixAAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			matrixRegs := []struct {
				base uint16
				reg  uint8
			}{
				{addr, 1},
				{addr + 2, 2},
				{addr + 4, 3},
				{addr + 6, 4},
			}
			for _, m := range matrixRegs {
				cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
				cg.builder.AddImmediate(m.base)
				cg.builder.AddInstruction(rom.EncodeMOV(0, 6, m.reg))
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
				cg.builder.AddImmediate(0x00FF)
				cg.builder.AddInstruction(rom.EncodeAND(0, 6, 7))
				cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

				cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
				cg.builder.AddImmediate(m.base + 1)
				cg.builder.AddInstruction(rom.EncodeMOV(0, 6, m.reg))
				cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
				cg.builder.AddImmediate(8)
				cg.builder.AddInstruction(rom.EncodeSHR(0, 6, 7))
				cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))
			}

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "matrix.set_center":
		// matrix.set_center(layer: u8, x: i16, y: i16)
		// Args: R0 = layer, R1 = centerX, R2 = centerY
		if len(args) != 3 {
			return fmt.Errorf("matrix.set_center requires 3 arguments (layer, x, y)")
		}
		centerAddrs := []uint16{0x8027, 0x8034, 0x8041, 0x804E}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range centerAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			centerRegs := []struct {
				base uint16
				reg  uint8
			}{
				{addr, 1},
				{addr + 2, 2},
			}
			for _, c := range centerRegs {
				cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
				cg.builder.AddImmediate(c.base)
				cg.builder.AddInstruction(rom.EncodeMOV(0, 5, c.reg))
				cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
				cg.builder.AddImmediate(0x00FF)
				cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
				cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

				cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
				cg.builder.AddImmediate(c.base + 1)
				cg.builder.AddInstruction(rom.EncodeMOV(0, 5, c.reg))
				cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
				cg.builder.AddImmediate(8)
				cg.builder.AddInstruction(rom.EncodeSHR(0, 5, 6))
				cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))
			}

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "matrix.identity":
		// matrix.identity(layer: u8)
		// Args: R0 = layer
		if len(args) != 1 {
			return fmt.Errorf("matrix.identity requires 1 argument (layer)")
		}
		matrixAAddrs := []uint16{0x8019, 0x802C, 0x8039, 0x8046}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range matrixAAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			values := []struct {
				base  uint16
				value uint16
			}{
				{addr, 0x0100},     // A
				{addr + 2, 0x0000}, // B
				{addr + 4, 0x0000}, // C
				{addr + 6, 0x0100}, // D
			}
			for _, v := range values {
				cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
				cg.builder.AddImmediate(v.base)
				cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
				cg.builder.AddImmediate(v.value & 0x00FF)
				cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

				cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
				cg.builder.AddImmediate(v.base + 1)
				cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
				cg.builder.AddImmediate(v.value >> 8)
				cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))
			}

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	case "matrix.set_flags":
		// matrix.set_flags(layer: u8, mirror_h: bool, mirror_v: bool, outside_mode: u8, direct_color: bool)
		// Args: R0 = layer, R1 = mirror_h, R2 = mirror_v, R3 = outside_mode, R4 = direct_color
		// Preserves the enable bit while rewriting control bits [5:1].
		if len(args) != 5 {
			return fmt.Errorf("matrix.set_flags requires 5 arguments (layer, mirror_h, mirror_v, outside_mode, direct_color)")
		}
		matrixControlAddrs := []uint16{0x8018, 0x802B, 0x8038, 0x8045}
		jumpToEnd := make([]int, 0, 4)
		for i, addr := range matrixControlAddrs {
			cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
			cg.builder.AddImmediate(uint16(i))
			cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 6))
			cg.builder.AddInstruction(rom.EncodeBNE())
			skipPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)

			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 5)) // R6 = current control
			cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
			cg.builder.AddImmediate(0x01)
			cg.builder.AddInstruction(rom.EncodeAND(0, 6, 7)) // R6 = enabled bit only

			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1)) // mirror_h
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x01)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(1)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeOR(0, 6, 7))

			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 2)) // mirror_v
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x01)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(2)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeOR(0, 6, 7))

			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 3)) // outside_mode
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x03)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(3)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeOR(0, 6, 7))

			cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 4)) // direct_color
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(0x01)
			cg.builder.AddInstruction(rom.EncodeAND(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(5)
			cg.builder.AddInstruction(rom.EncodeSHL(0, 7, 5))
			cg.builder.AddInstruction(rom.EncodeOR(0, 6, 7))

			cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
			cg.builder.AddImmediate(addr)
			cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

			cg.builder.AddInstruction(rom.EncodeJMP())
			jumpPos := cg.builder.GetCodeLength()
			cg.builder.AddImmediate(0)
			jumpToEnd = append(jumpToEnd, jumpPos)

			nextPC := uint16(cg.builder.GetCodeLength() * 2)
			skipPC := uint16(skipPos * 2)
			cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
		}
		endPC := uint16(cg.builder.GetCodeLength() * 2)
		for _, jp := range jumpToEnd {
			jpPC := uint16(jp * 2)
			cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
		}
		return nil

	default:
		return fmt.Errorf("%w: %s", errUnknownBuiltin, name)
	}
}

// generateRuntimeTileLoadDispatch generates runtime asset-ID dispatch for gfx.load_tiles.
// Assumes R0=assetID, R1=base tile index.
func (cg *CodeGenerator) generateRuntimeTileLoadDispatch(destReg uint8) error {
	// Preserve runtime inputs across case probes.
	cg.builder.AddInstruction(rom.EncodeMOV(0, 2, 1)) // MOV R2, R1 (base)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 0)) // MOV R3, R0 (asset id)

	jumpToEnd := make([]int, 0, len(cg.program.Assets))

	for _, asset := range cg.program.Assets {
		assetID, ok := cg.assetIDs[asset.Name]
		if !ok {
			return fmt.Errorf("missing runtime asset ID for %s", asset.Name)
		}

		// if R3 != assetID -> skip this case
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #assetID
		cg.builder.AddImmediate(assetID)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 3, 4)) // CMP R3, R4
		cg.builder.AddInstruction(rom.EncodeBNE())
		skipOffsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0) // patch after case body

		// Matched case: restore base to R1 and inline-load the chosen asset.
		cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 2)) // MOV R1, R2
		if err := cg.generateInlineTileLoadFromBaseReg(asset, 1, destReg); err != nil {
			return err
		}

		// Jump to common end after handling this match.
		cg.builder.AddInstruction(rom.EncodeJMP())
		jumpPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		jumpToEnd = append(jumpToEnd, jumpPos)

		nextCasePC := uint16(cg.builder.GetCodeLength() * 2)
		currentPC := uint16(skipOffsetPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, nextCasePC)
		cg.builder.SetImmediateAt(skipOffsetPos, uint16(offset))
	}

	// Unknown runtime ID: leave VRAM unchanged and return base.
	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 2)) // MOV R{destReg}, R2

	endPC := uint16(cg.builder.GetCodeLength() * 2)
	for _, jumpPos := range jumpToEnd {
		currentPC := uint16(jumpPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, endPC)
		cg.builder.SetImmediateAt(jumpPos, uint16(offset))
	}

	return nil
}

func (cg *CodeGenerator) generateRuntimeTilemapLoadDispatch(destReg uint8) error {
	// Runtime asset-ID dispatch for bg.load_tilemap(asset, layer).
	cg.builder.AddInstruction(rom.EncodeMOV(0, 2, 1)) // R2 = layer (preserve)

	tilemapAssets := make([]*AssetDecl, 0)
	for _, asset := range cg.program.Assets {
		if asset.Type == "tilemap" {
			tilemapAssets = append(tilemapAssets, asset)
		}
	}
	if len(tilemapAssets) == 0 {
		return fmt.Errorf("bg.load_tilemap requires at least one tilemap asset")
	}

	jumpToEnd := make([]int, 0, len(tilemapAssets))
	for _, asset := range tilemapAssets {
		value, ok := cg.assetIDs[asset.Name]
		if !ok {
			continue
		}

		cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
		cg.builder.AddImmediate(value)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 3))
		cg.builder.AddInstruction(rom.EncodeBNE())
		skipOffsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 2)) // restore layer to R1
		if err := cg.generateInlineTilemapLoadFromLayerReg(asset, 1, destReg); err != nil {
			return err
		}

		cg.builder.AddInstruction(rom.EncodeJMP())
		jumpPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		jumpToEnd = append(jumpToEnd, jumpPos)

		nextCasePC := uint16(cg.builder.GetCodeLength() * 2)
		currentPC := uint16(skipOffsetPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, nextCasePC)
		cg.builder.SetImmediateAt(skipOffsetPos, uint16(offset))
	}

	// Unknown runtime ID: return 0.
	cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
	cg.builder.AddImmediate(0)

	endPC := uint16(cg.builder.GetCodeLength() * 2)
	for _, jumpPos := range jumpToEnd {
		currentPC := uint16(jumpPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, endPC)
		cg.builder.SetImmediateAt(jumpPos, uint16(offset))
	}

	return nil
}

// generateRuntimeMatrixPlaneTileLoadDispatch generates runtime asset-ID dispatch
// for matrix_plane.load_tiles. Assumes R0=assetID, R1=channel, R2=base tile index.
func (cg *CodeGenerator) generateRuntimeMatrixPlaneTileLoadDispatch(destReg uint8) error {
	cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 1)) // R3 = channel
	cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 2)) // R4 = base

	jumpToEnd := make([]int, 0, len(cg.program.Assets))

	for _, asset := range cg.program.Assets {
		assetID, ok := cg.assetIDs[asset.Name]
		if !ok {
			return fmt.Errorf("missing runtime asset ID for %s", asset.Name)
		}

		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(assetID)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 5))
		cg.builder.AddInstruction(rom.EncodeBNE())
		skipOffsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 3)) // restore channel to R1
		cg.builder.AddInstruction(rom.EncodeMOV(0, 2, 4)) // restore base to R2
		if err := cg.generateInlineMatrixPlaneTileLoadFromRegs(asset, 1, 2, destReg); err != nil {
			return err
		}

		cg.builder.AddInstruction(rom.EncodeJMP())
		jumpPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		jumpToEnd = append(jumpToEnd, jumpPos)

		nextCasePC := uint16(cg.builder.GetCodeLength() * 2)
		currentPC := uint16(skipOffsetPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, nextCasePC)
		cg.builder.SetImmediateAt(skipOffsetPos, uint16(offset))
	}

	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 4)) // return base unchanged

	endPC := uint16(cg.builder.GetCodeLength() * 2)
	for _, jumpPos := range jumpToEnd {
		currentPC := uint16(jumpPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, endPC)
		cg.builder.SetImmediateAt(jumpPos, uint16(offset))
	}
	return nil
}

func (cg *CodeGenerator) generateRuntimeMatrixPlaneTilemapLoadDispatch(destReg uint8) error {
	cg.builder.AddInstruction(rom.EncodeMOV(0, 2, 1)) // R2 = channel

	tilemapAssets := make([]*AssetDecl, 0)
	for _, asset := range cg.program.Assets {
		if asset.Type == "tilemap" {
			tilemapAssets = append(tilemapAssets, asset)
		}
	}
	if len(tilemapAssets) == 0 {
		return fmt.Errorf("matrix_plane.load_tilemap requires at least one tilemap asset")
	}

	jumpToEnd := make([]int, 0, len(tilemapAssets))
	for _, asset := range tilemapAssets {
		value, ok := cg.assetIDs[asset.Name]
		if !ok {
			continue
		}

		cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
		cg.builder.AddImmediate(value)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 0, 3))
		cg.builder.AddInstruction(rom.EncodeBNE())
		skipOffsetPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 2)) // restore channel to R1
		if err := cg.generateInlineMatrixPlaneTilemapLoadFromChannelReg(asset, 1, destReg); err != nil {
			return err
		}

		cg.builder.AddInstruction(rom.EncodeJMP())
		jumpPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		jumpToEnd = append(jumpToEnd, jumpPos)

		nextCasePC := uint16(cg.builder.GetCodeLength() * 2)
		currentPC := uint16(skipOffsetPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, nextCasePC)
		cg.builder.SetImmediateAt(skipOffsetPos, uint16(offset))
	}

	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 2)) // return channel unchanged

	endPC := uint16(cg.builder.GetCodeLength() * 2)
	for _, jumpPos := range jumpToEnd {
		currentPC := uint16(jumpPos * 2)
		offset := rom.CalculateBranchOffset(currentPC, endPC)
		cg.builder.SetImmediateAt(jumpPos, uint16(offset))
	}
	return nil
}

// generateInlineTileLoad generates code to load tile data from an asset directly to VRAM
func (cg *CodeGenerator) generateInlineTileLoad(asset *AssetDecl, baseExpr Expr, destReg uint8) error {
	if asset.Type != "tiles8" && asset.Type != "tiles16" && asset.Type != "sprite" && asset.Type != "tileset" {
		return fmt.Errorf("gfx.load_tiles requires tile asset type, got %s", asset.Type)
	}
	// Generate base tile index (second argument)
	if err := cg.generateExpr(baseExpr, 1); err != nil {
		return err
	}
	return cg.generateInlineTileLoadFromBaseReg(asset, 1, destReg)
}

func (cg *CodeGenerator) generateInlineTileLoadFromBaseReg(asset *AssetDecl, baseReg uint8, destReg uint8) error {
	// Calculate VRAM address from the base tile index.
	cg.builder.AddInstruction(rom.EncodeMOV(0, 2, baseReg)) // MOV R2, R{baseReg} (save base)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 2))       // MOV R3, R2 (working copy)

	dataBytes, err := cg.inlineTileAssetBytes(asset)
	if err != nil {
		return err
	}
	bytesPayload := len(dataBytes)

	use16x16Stride := asset.Type == "tiles16" || (asset.Type == "tileset" && bytesPayload == 128)
	if use16x16Stride {
		// 16x16 tile: base * 128 = base << 7 (PPU reads sprite tile at index*128)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #7
		cg.builder.AddImmediate(7)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 3, 4))
	} else {
		// 8x8 tile: base * 32 = base << 5
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #5
		cg.builder.AddImmediate(5)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 3, 4))
	}

	// Set VRAM address low/high registers.
	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x800E
	cg.builder.AddImmediate(0x800E)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 3))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #0xFF
	cg.builder.AddImmediate(0xFF)
	cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x800F
	cg.builder.AddImmediate(0x800F)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 3))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0)) // MOV R6, #8
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeSHR(0, 5, 6))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0)) // MOV R4, #0x8010 (VRAM_DATA)
	cg.builder.AddImmediate(0x8010)

	bytesToWrite := bytesPayload
	switch asset.Type {
	case "tiles8":
		// tiles8 maps to a single 8x8 tile payload.
		if bytesToWrite > 32 {
			bytesToWrite = 32
		}
	case "tiles16":
		// tiles16 maps to a single 16x16 tile payload.
		if bytesToWrite > 128 {
			bytesToWrite = 128
		}
	default:
		// sprite/tileset payloads may represent multiple contiguous tiles.
		// Write the full normalized payload so tools can emit larger data blocks.
	}
	for i, value := range dataBytes {
		if i >= bytesToWrite {
			break
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0)) // MOV R5, #value
		cg.builder.AddImmediate(uint16(value))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))
	}

	// Return base tile index.
	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 2))
	return nil
}

func (cg *CodeGenerator) generateInlineMatrixPlaneTileLoadFromRegs(asset *AssetDecl, channelReg, baseReg uint8, destReg uint8) error {
	if asset.Type != "tiles8" && asset.Type != "tiles16" && asset.Type != "sprite" && asset.Type != "tileset" {
		return fmt.Errorf("matrix_plane.load_tiles requires tile asset type, got %s", asset.Type)
	}
	dataBytes, err := cg.inlineTileAssetBytes(asset)
	if err != nil {
		return err
	}
	bytesPayload := len(dataBytes)
	use16x16Stride := asset.Type == "tiles16" || (asset.Type == "tileset" && bytesPayload == 128)

	// Select the plane first.
	cg.emitSelectMatrixPlane(channelReg, 3, 4)

	// Calculate pattern address from tile index.
	cg.builder.AddInstruction(rom.EncodeMOV(0, 3, baseReg)) // R3 = base
	if use16x16Stride {
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
		cg.builder.AddImmediate(7)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 3, 4))
	} else {
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
		cg.builder.AddImmediate(5)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 3, 4))
	}

	// Set pattern address registers.
	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(0x8085)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 3))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeAND(0, 5, 6))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(0x8086)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 5, 3))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeSHR(0, 5, 6))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(0x8087)

	bytesToWrite := bytesPayload
	switch asset.Type {
	case "tiles8":
		if bytesToWrite > 32 {
			bytesToWrite = 32
		}
	case "tiles16":
		if bytesToWrite > 128 {
			bytesToWrite = 128
		}
	}
	for i, value := range dataBytes {
		if i >= bytesToWrite {
			break
		}
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(uint16(value))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 4, 5))
	}

	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, baseReg))
	return nil
}

func (cg *CodeGenerator) generateInlineMatrixPlaneTilemapLoadFromChannelReg(asset *AssetDecl, channelReg uint8, destReg uint8) error {
	if asset.Type != "tilemap" {
		return fmt.Errorf("matrix_plane.load_tilemap requires tilemap asset type, got %s", asset.Type)
	}
	dataBytes, err := cg.inlineTilemapAssetBytes(asset)
	if err != nil {
		return err
	}

	cg.emitSelectMatrixPlane(channelReg, 3, 4)

	cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
	cg.builder.AddImmediate(0x8082)
	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 3, 4))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
	cg.builder.AddImmediate(0x8083)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 3, 4))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
	cg.builder.AddImmediate(0x8084)
	for _, value := range dataBytes {
		cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
		cg.builder.AddImmediate(uint16(value))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 3, 4))
	}

	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, channelReg))
	return nil
}

func (cg *CodeGenerator) generateMatrixPlaneFillRect() error {
	xAddr, err := cg.allocateStack(2, "matrix plane fill rect x start")
	if err != nil {
		return err
	}
	yAddr, err := cg.allocateStack(2, "matrix plane fill rect y current")
	if err != nil {
		return err
	}
	wAddr, err := cg.allocateStack(2, "matrix plane fill rect width")
	if err != nil {
		return err
	}
	hAddr, err := cg.allocateStack(2, "matrix plane fill rect height")
	if err != nil {
		return err
	}
	tileAddr, err := cg.allocateStack(2, "matrix plane fill rect tile")
	if err != nil {
		return err
	}
	attrAddr, err := cg.allocateStack(2, "matrix plane fill rect attr")
	if err != nil {
		return err
	}
	shiftAddr, err := cg.allocateStack(2, "matrix plane fill rect shift")
	if err != nil {
		return err
	}

	// Spill arguments needed across the nested loops.
	cg.emitStoreStackWord(1, xAddr)
	cg.emitStoreStackWord(2, yAddr)
	cg.emitStoreStackWord(3, wAddr)
	cg.emitStoreStackWord(4, hAddr)
	cg.emitStoreStackWord(5, tileAddr)
	cg.emitStoreStackWord(6, attrAddr)

	cg.emitSelectMatrixPlane(0, 6, 5)
	cg.emitSelectedMatrixPlaneShift(5, 6, 4)
	cg.emitStoreStackWord(5, shiftAddr)

	rowLoopStart := cg.builder.GetCodeLength()
	cg.emitLoadStackWord(4, hAddr) // R4 = remaining rows
	cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 4, 0))
	cg.builder.AddInstruction(rom.EncodeBEQ())
	rowLoopEndPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)

	cg.emitLoadStackWord(1, yAddr) // R1 = current y
	cg.emitLoadStackWord(2, xAddr) // R2 = current x
	cg.emitLoadStackWord(3, wAddr) // R3 = remaining cols

	colLoopStart := cg.builder.GetCodeLength()
	cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 3, 0))
	cg.builder.AddInstruction(rom.EncodeBEQ())
	colLoopEndPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)

	// offset = ((y << shift) + x) << 1
	cg.builder.AddInstruction(rom.EncodeMOV(0, 4, 1)) // R4 = y
	cg.emitLoadStackWord(5, shiftAddr)
	cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5))
	cg.builder.AddInstruction(rom.EncodeADD(0, 4, 2))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(1)
	cg.builder.AddInstruction(rom.EncodeSHL(0, 4, 5))

	// Address low/high
	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(0x8082)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 4))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeAND(0, 6, 7))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(0x8083)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 4))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeSHR(0, 6, 7))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

	// Data writes
	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(0x8084)
	cg.emitLoadStackWord(6, tileAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))
	cg.emitLoadStackWord(6, attrAddr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

	// x++, remaining cols--
	cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
	cg.builder.AddImmediate(1)
	cg.builder.AddInstruction(rom.EncodeADD(0, 2, 0))
	cg.builder.AddInstruction(rom.EncodeSUB(0, 3, 0))
	cg.builder.AddInstruction(rom.EncodeJMP())
	colLoopJumpPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(uint16(colLoopJumpPos*2), uint16(colLoopStart*2))))

	colLoopEndPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(colLoopEndPos, uint16(rom.CalculateBranchOffset(uint16(colLoopEndPos*2), colLoopEndPC)))

	// y++, remaining rows--
	cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
	cg.builder.AddImmediate(1)
	cg.builder.AddInstruction(rom.EncodeADD(0, 1, 0))
	cg.emitStoreStackWord(1, yAddr)
	cg.emitLoadStackWord(4, hAddr)
	cg.builder.AddInstruction(rom.EncodeSUB(0, 4, 0))
	cg.emitStoreStackWord(4, hAddr)
	cg.builder.AddInstruction(rom.EncodeJMP())
	rowLoopJumpPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(uint16(rowLoopJumpPos*2), uint16(rowLoopStart*2))))

	rowLoopEndPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(rowLoopEndPos, uint16(rom.CalculateBranchOffset(uint16(rowLoopEndPos*2), rowLoopEndPC)))

	return nil
}

func (cg *CodeGenerator) generateMatrixPlaneClear() error {
	// Args: R0 = channel, R1 = tile, R2 = attr
	cg.emitSelectMatrixPlane(0, 6, 5)
	cg.emitSelectedMatrixPlaneShift(5, 6, 4) // shift in R5

	// total entries based on selected plane size:
	// 32x32 -> 1024, 64x64 -> 4096, 128x128 -> 16384
	cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
	cg.builder.AddImmediate(1024)
	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(6)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 5, 4))
	cg.builder.AddInstruction(rom.EncodeBNE())
	not64Pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
	cg.builder.AddImmediate(4096)
	cg.builder.AddInstruction(rom.EncodeJMP())
	after64Pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	not64PC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(not64Pos, uint16(rom.CalculateBranchOffset(uint16(not64Pos*2), not64PC)))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(7)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 5, 4))
	cg.builder.AddInstruction(rom.EncodeBNE())
	not128Pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(1, 3, 0))
	cg.builder.AddImmediate(16384)
	not128PC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(not128Pos, uint16(rom.CalculateBranchOffset(uint16(not128Pos*2), not128PC)))

	after64PC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(after64Pos, uint16(rom.CalculateBranchOffset(uint16(after64Pos*2), after64PC)))

	// zero tilemap address
	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(0x8082)
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(0x8083)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 6))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
	cg.builder.AddImmediate(0x8084)
	loopStart := cg.builder.GetCodeLength()
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeCMP(0, 3, 6))
	cg.builder.AddInstruction(rom.EncodeBEQ())
	loopEndPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 1))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 5, 2))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(1)
	cg.builder.AddInstruction(rom.EncodeSUB(0, 3, 6))
	cg.builder.AddInstruction(rom.EncodeJMP())
	loopJumpPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(uint16(loopJumpPos*2), uint16(loopStart*2))))
	loopEndPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(loopEndPos, uint16(rom.CalculateBranchOffset(uint16(loopEndPos*2), loopEndPC)))
	return nil
}

func (cg *CodeGenerator) generateInlineTilemapLoad(asset *AssetDecl, layerExpr Expr, destReg uint8) error {
	if asset.Type != "tilemap" {
		return fmt.Errorf("bg.load_tilemap requires tilemap asset type, got %s", asset.Type)
	}
	if err := cg.generateExpr(layerExpr, 1); err != nil {
		return err
	}
	return cg.generateInlineTilemapLoadFromLayerReg(asset, 1, destReg)
}

func (cg *CodeGenerator) generateInlineTilemapLoadFromLayerReg(asset *AssetDecl, layerReg uint8, destReg uint8) error {
	if asset.Type != "tilemap" {
		return fmt.Errorf("bg.load_tilemap requires tilemap asset type, got %s", asset.Type)
	}
	dataBytes, err := cg.inlineTilemapAssetBytes(asset)
	if err != nil {
		return err
	}

	// Resolve the configured tilemap base for the selected layer into R5.
	bgTilemapAddrs := []uint16{0x8077, 0x8079, 0x807B, 0x807D}
	jumpToEnd := make([]int, 0, 4)
	for i, addr := range bgTilemapAddrs {
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(uint16(i))
		cg.builder.AddInstruction(rom.EncodeCMP(0, layerReg, 6))
		cg.builder.AddInstruction(rom.EncodeBNE())
		skipPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)

		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(addr)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 5, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
		cg.builder.AddImmediate(addr + 1)
		cg.builder.AddInstruction(rom.EncodeMOV(2, 6, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(8)
		cg.builder.AddInstruction(rom.EncodeSHL(0, 6, 7))
		cg.builder.AddInstruction(rom.EncodeOR(0, 5, 6))
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeCMP(0, 5, 7))
		cg.builder.AddInstruction(rom.EncodeBNE())
		baseReadyPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(1, 5, 0))
		cg.builder.AddImmediate(0x4000)
		baseReadyPC := uint16(cg.builder.GetCodeLength() * 2)
		cg.builder.SetImmediateAt(baseReadyPos, uint16(rom.CalculateBranchOffset(uint16(baseReadyPos*2), baseReadyPC)))

		cg.builder.AddInstruction(rom.EncodeJMP())
		jumpPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		jumpToEnd = append(jumpToEnd, jumpPos)

		nextPC := uint16(cg.builder.GetCodeLength() * 2)
		skipPC := uint16(skipPos * 2)
		cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
	}
	endPC := uint16(cg.builder.GetCodeLength() * 2)
	for _, jp := range jumpToEnd {
		jpPC := uint16(jp * 2)
		cg.builder.SetImmediateAt(jp, uint16(rom.CalculateBranchOffset(jpPC, endPC)))
	}

	// Program VRAM address from resolved base.
	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(0x800E)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeAND(0, 7, 4))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(0x800F)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 5))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 4, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeSHR(0, 7, 4))
	cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))

	cg.builder.AddInstruction(rom.EncodeMOV(1, 6, 0))
	cg.builder.AddImmediate(0x8010)
	for _, value := range dataBytes {
		cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
		cg.builder.AddImmediate(uint16(value))
		cg.builder.AddInstruction(rom.EncodeMOV(3, 6, 7))
	}

	// Return the tilemap base used for the transfer.
	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 5))
	return nil
}

func (cg *CodeGenerator) inlineTileAssetBytes(asset *AssetDecl) ([]byte, error) {
	if norm, ok := cg.normalizedAssets[asset.Name]; ok {
		return norm.Data, nil
	}
	// Fallback for direct codegen use outside the compiler pipeline.
	if asset.Encoding != "hex" {
		return nil, fmt.Errorf("inline tile load requires normalized asset data for %s encoding", asset.Encoding)
	}
	return decodeHexAssetData(asset.Data)
}

func (cg *CodeGenerator) inlineTilemapAssetBytes(asset *AssetDecl) ([]byte, error) {
	if norm, ok := cg.normalizedAssets[asset.Name]; ok {
		return norm.Data, nil
	}
	switch asset.Encoding {
	case "hex":
		return decodeHexAssetData(asset.Data)
	case "b64":
		return decodeBase64AssetData(asset.Data)
	default:
		return nil, fmt.Errorf("inline tilemap load requires normalized asset data for %s encoding", asset.Encoding)
	}
}

func (cg *CodeGenerator) emitLoadMMIO8(destReg uint8, addr uint16) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0))
	cg.builder.AddImmediate(addr)
	cg.builder.AddInstruction(rom.EncodeMOV(2, destReg, destReg))
}

func (cg *CodeGenerator) emitLoadStackWord(destReg uint8, addr uint16) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(addr)
	cg.builder.AddInstruction(rom.EncodeMOV(2, destReg, 7))
}

func (cg *CodeGenerator) emitStoreStackWord(srcReg uint8, addr uint16) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, 7, 0))
	cg.builder.AddImmediate(addr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 7, srcReg))
}

func (cg *CodeGenerator) emitSelectMatrixPlane(channelReg, addrReg, tmpReg uint8) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, addrReg, 0))
	cg.builder.AddImmediate(0x8080)
	cg.builder.AddInstruction(rom.EncodeMOV(0, tmpReg, channelReg))
	cg.builder.AddInstruction(rom.EncodeMOV(1, 0, 0))
	cg.builder.AddImmediate(0x03)
	cg.builder.AddInstruction(rom.EncodeAND(0, tmpReg, 0))
	cg.builder.AddInstruction(rom.EncodeMOV(3, addrReg, tmpReg))
}

func (cg *CodeGenerator) emitSelectedMatrixPlaneShift(shiftReg, controlReg, scratchReg uint8) {
	cg.emitLoadMMIO8(controlReg, 0x8081)
	cg.builder.AddInstruction(rom.EncodeMOV(1, scratchReg, 0))
	cg.builder.AddImmediate(1)
	cg.builder.AddInstruction(rom.EncodeSHR(0, controlReg, scratchReg))
	cg.builder.AddInstruction(rom.EncodeMOV(1, scratchReg, 0))
	cg.builder.AddImmediate(0x03)
	cg.builder.AddInstruction(rom.EncodeAND(0, controlReg, scratchReg))
	cg.builder.AddInstruction(rom.EncodeMOV(1, shiftReg, 0))
	cg.builder.AddImmediate(5)

	cg.builder.AddInstruction(rom.EncodeMOV(1, scratchReg, 0))
	cg.builder.AddImmediate(1)
	cg.builder.AddInstruction(rom.EncodeCMP(0, controlReg, scratchReg))
	cg.builder.AddInstruction(rom.EncodeBNE())
	not64Pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(1, shiftReg, 0))
	cg.builder.AddImmediate(6)
	cg.builder.AddInstruction(rom.EncodeJMP())
	after64Pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	not64PC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(not64Pos, uint16(rom.CalculateBranchOffset(uint16(not64Pos*2), not64PC)))

	cg.builder.AddInstruction(rom.EncodeMOV(1, scratchReg, 0))
	cg.builder.AddImmediate(2)
	cg.builder.AddInstruction(rom.EncodeCMP(0, controlReg, scratchReg))
	cg.builder.AddInstruction(rom.EncodeBNE())
	not128Pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	cg.builder.AddInstruction(rom.EncodeMOV(1, shiftReg, 0))
	cg.builder.AddImmediate(7)
	not128PC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(not128Pos, uint16(rom.CalculateBranchOffset(uint16(not128Pos*2), not128PC)))

	after64PC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(after64Pos, uint16(rom.CalculateBranchOffset(uint16(after64Pos*2), after64PC)))
}

func (cg *CodeGenerator) emitMaybeAddShiftedScanlineIfMMIOFlag(mmioAddr, mask, shiftImm uint16, scanlineReg, destReg, scratchReg uint8) {
	cg.emitLoadMMIO8(scratchReg, mmioAddr)
	cg.builder.AddInstruction(rom.EncodeAND(1, scratchReg, 0))
	cg.builder.AddImmediate(mask)
	cg.builder.AddInstruction(rom.EncodeBEQ())
	skipPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)

	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, scanlineReg))
	if shiftImm != 0 {
		cg.builder.AddInstruction(rom.EncodeSHL(1, scratchReg, 0))
		cg.builder.AddImmediate(shiftImm)
	}
	cg.builder.AddInstruction(rom.EncodeADD(0, destReg, scratchReg))

	skipPC := uint16(skipPos * 2)
	nextPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
}

func (cg *CodeGenerator) emitMaybeAddImmediateIfMMIOFlag(mmioAddr, mask, addImm uint16, destReg, scratchReg uint8) {
	cg.emitLoadMMIO8(scratchReg, mmioAddr)
	cg.builder.AddInstruction(rom.EncodeAND(1, scratchReg, 0))
	cg.builder.AddImmediate(mask)
	cg.builder.AddInstruction(rom.EncodeBEQ())
	skipPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)

	cg.builder.AddInstruction(rom.EncodeADD(1, destReg, 0))
	cg.builder.AddImmediate(addImm)

	skipPC := uint16(skipPos * 2)
	nextPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(skipPos, uint16(rom.CalculateBranchOffset(skipPC, nextPC)))
}

func (cg *CodeGenerator) emitComputeRasterScanlineBase(scanlineReg, destReg, scratchReg uint8) {
	cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, scanlineReg))
	cg.builder.AddInstruction(rom.EncodeSHL(1, destReg, 0))
	cg.builder.AddImmediate(6)
	cg.emitMaybeAddShiftedScanlineIfMMIOFlag(0x805D, 0x20, 2, scanlineReg, destReg, scratchReg)
	cg.emitMaybeAddShiftedScanlineIfMMIOFlag(0x805D, 0x40, 2, scanlineReg, destReg, scratchReg)
	cg.emitMaybeAddShiftedScanlineIfMMIOFlag(0x805D, 0x80, 3, scanlineReg, destReg, scratchReg)
	cg.emitMaybeAddShiftedScanlineIfMMIOFlag(0x807F, 0x01, 2, scanlineReg, destReg, scratchReg)

	cg.emitLoadMMIO8(scratchReg, 0x805E)
	cg.builder.AddInstruction(rom.EncodeADD(0, destReg, scratchReg))
	cg.emitLoadMMIO8(scratchReg, 0x805F)
	cg.builder.AddInstruction(rom.EncodeSHL(1, scratchReg, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeADD(0, destReg, scratchReg))
}

func (cg *CodeGenerator) emitWriteVRAM16AtAddrReg(addrReg, valueReg, scratchReg, ioReg uint8) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, ioReg, 0))
	cg.builder.AddImmediate(0x800E)
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, addrReg))
	cg.builder.AddInstruction(rom.EncodeAND(1, scratchReg, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))

	cg.builder.AddInstruction(rom.EncodeMOV(1, ioReg, 0))
	cg.builder.AddImmediate(0x800F)
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, addrReg))
	cg.builder.AddInstruction(rom.EncodeSHR(1, scratchReg, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))

	cg.builder.AddInstruction(rom.EncodeMOV(1, ioReg, 0))
	cg.builder.AddImmediate(0x8010)
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, valueReg))
	cg.builder.AddInstruction(rom.EncodeAND(1, scratchReg, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, valueReg))
	cg.builder.AddInstruction(rom.EncodeSHR(1, scratchReg, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))
}

func (cg *CodeGenerator) emitWriteVRAM8AtAddrReg(addrReg, valueReg, scratchReg, ioReg uint8) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, ioReg, 0))
	cg.builder.AddImmediate(0x800E)
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, addrReg))
	cg.builder.AddInstruction(rom.EncodeAND(1, scratchReg, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))

	cg.builder.AddInstruction(rom.EncodeMOV(1, ioReg, 0))
	cg.builder.AddImmediate(0x800F)
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, addrReg))
	cg.builder.AddInstruction(rom.EncodeSHR(1, scratchReg, 0))
	cg.builder.AddImmediate(8)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))

	cg.builder.AddInstruction(rom.EncodeMOV(1, ioReg, 0))
	cg.builder.AddImmediate(0x8010)
	cg.builder.AddInstruction(rom.EncodeMOV(0, scratchReg, valueReg))
	cg.builder.AddInstruction(rom.EncodeAND(1, scratchReg, 0))
	cg.builder.AddImmediate(0x00FF)
	cg.builder.AddInstruction(rom.EncodeMOV(3, ioReg, scratchReg))
}

func (cg *CodeGenerator) generateMember(expr *MemberExpr, destReg uint8) error {
	// Generate object
	if err := cg.generateExpr(expr.Object, 0); err != nil {
		return err
	}
	// Member access would need struct layout knowledge
	return fmt.Errorf("member access not fully implemented: %s", expr.Member)
}

func (cg *CodeGenerator) generateUnary(expr *UnaryExpr, destReg uint8) error {
	if err := cg.generateExpr(expr.Operand, destReg); err != nil {
		return err
	}
	switch expr.Op {
	case TOKEN_MINUS:
		// Negate: 0 - value
		cg.builder.AddInstruction(rom.EncodeMOV(1, 1, 0)) // MOV R1, #0
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeSUB(0, 1, destReg)) // SUB R1, R{destReg}
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // MOV R{destReg}, R1
	case TOKEN_NOT:
		// Logical NOT: compare with 0, set to 1 if zero, 0 otherwise
		// Compare operand with 0
		cg.builder.AddInstruction(rom.EncodeMOV(1, 1, 0)) // MOV R1, #0
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeCMP(0, destReg, 1)) // CMP R{destReg}, R1
		// Set to 1 if equal (zero), 0 if not equal
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // MOV R{destReg}, #0
		cg.builder.AddImmediate(0)
		skipLabel := cg.newLabel()
		cg.builder.AddInstruction(rom.EncodeBNE()) // BNE skip
		skipPos := cg.builder.GetCodeLength()
		cg.builder.AddImmediate(0)
		cg.builder.AddInstruction(rom.EncodeMOV(1, destReg, 0)) // MOV R{destReg}, #1
		cg.builder.AddImmediate(1)
		cg.patchLabel(skipLabel, skipPos)
		return nil
	case TOKEN_TILDE:
		// Bitwise NOT: 0xFFFF - value
		cg.builder.AddInstruction(rom.EncodeMOV(1, 1, 0)) // MOV R1, #0xFFFF
		cg.builder.AddImmediate(0xFFFF)
		cg.builder.AddInstruction(rom.EncodeSUB(0, 1, destReg)) // SUB R1, R{destReg}
		cg.builder.AddInstruction(rom.EncodeMOV(0, destReg, 1)) // MOV R{destReg}, R1
		return nil
	case TOKEN_AMPERSAND:
		// Address-of operator &x
		// For now, simplified - just return 0 as placeholder
		// In full implementation, would return actual address
		// The operand is already evaluated, so we just use it
		return nil
	default:
		return fmt.Errorf("unary operator not yet implemented: %v", expr.Op)
	}
	return nil
}

func (cg *CodeGenerator) generateStore(target Expr, srcReg uint8) error {
	// Store value in srcReg to target
	// This is simplified
	return fmt.Errorf("store not fully implemented")
}

func (cg *CodeGenerator) allocateStack(bytes uint16, what string) (uint16, error) {
	if bytes == 0 {
		return cg.stackOffset, nil
	}
	if uint32(cg.stackOffset) < uint32(stackMinAddr)+uint32(bytes) {
		return 0, fmt.Errorf(
			"stack exhausted while allocating %s (%d bytes, SP=0x%04X, floor=0x%04X)",
			what, bytes, cg.stackOffset, stackMinAddr,
		)
	}
	cg.stackOffset -= bytes
	return cg.stackOffset, nil
}

func (cg *CodeGenerator) newLabel() int {
	label := cg.labelCounter
	cg.labelCounter++
	return label
}

func (cg *CodeGenerator) patchLabel(label, offsetPos int) {
	_ = label // Labels are currently patched immediately at their definition point.
	// offsetPos is the word index where the branch/jump immediate placeholder was emitted.
	// The CPU PC-relative branch offset is calculated from the address *after* the immediate.
	currentPC := uint16(offsetPos * 2)
	targetPC := uint16(cg.builder.GetCodeLength() * 2)
	offset := rom.CalculateBranchOffset(currentPC, targetPC)
	cg.builder.SetImmediateAt(offsetPos, uint16(offset))
}

// Scratch slots inside the reserved runtime block used by the software
// arithmetic helpers (the block at runtimeBlockBase is compiler-owned).
const (
	scratchIndexStore = runtimeBlockBase + 0x00 // array-store value stash
	inputCurrSlot     = runtimeBlockBase + 0x20 // input.poll current frame state
	inputPrevSlot     = runtimeBlockBase + 0x22 // input.poll previous frame state
)

// emitHelperCall emits a CALL to a named helper routine, patched after all
// code is generated (same mechanism as user function calls).
func (cg *CodeGenerator) emitHelperCall(name string) {
	cg.builder.AddInstruction(rom.EncodeCALL())
	offsetPos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	cg.callPatches = append(cg.callPatches, callPatch{offsetPos: offsetPos, target: name})
}

// helper emit primitives
func (cg *CodeGenerator) hMovImm(reg uint8, v uint16) {
	cg.builder.AddInstruction(rom.EncodeMOV(1, reg, 0))
	cg.builder.AddImmediate(v)
}
func (cg *CodeGenerator) hStore16(addr uint16, src uint8) {
	cg.hMovImm(7, addr)
	cg.builder.AddInstruction(rom.EncodeMOV(3, 7, src))
}
func (cg *CodeGenerator) hLoad16(dst uint8, addr uint16) {
	cg.hMovImm(7, addr)
	cg.builder.AddInstruction(rom.EncodeMOV(2, dst, 7))
}
func (cg *CodeGenerator) hAndImm(reg uint8, v uint16) {
	cg.builder.AddInstruction(rom.EncodeAND(1, reg, 0))
	cg.builder.AddImmediate(v)
}
func (cg *CodeGenerator) hShrImm(reg uint8, v uint16) {
	cg.builder.AddInstruction(rom.EncodeSHR(1, reg, 0))
	cg.builder.AddImmediate(v)
}
func (cg *CodeGenerator) hShlImm(reg uint8, v uint16) {
	cg.builder.AddInstruction(rom.EncodeSHL(1, reg, 0))
	cg.builder.AddImmediate(v)
}
func (cg *CodeGenerator) hCmpImm(reg uint8, v uint16) {
	cg.builder.AddInstruction(rom.EncodeCMP(7, reg, 0))
	cg.builder.AddImmediate(v)
}

// hBranch emits a conditional branch with a placeholder offset and returns
// the immediate position for later patching.
func (cg *CodeGenerator) hBranch(inst uint16) int {
	cg.builder.AddInstruction(inst)
	pos := cg.builder.GetCodeLength()
	cg.builder.AddImmediate(0)
	return pos
}

func (cg *CodeGenerator) hPatchToHere(immPos int) {
	herePC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.SetImmediateAt(immPos, uint16(rom.CalculateBranchOffset(uint16(immPos*2), herePC)))
}

func (cg *CodeGenerator) hJumpBack(toWordIdx int) {
	cg.builder.AddInstruction(rom.EncodeJMP())
	fromPC := uint16(cg.builder.GetCodeLength() * 2)
	cg.builder.AddImmediate(uint16(rom.CalculateBranchOffset(fromPC, uint16(toWordIdx*2))))
}

// emitFixmulHelper emits __fixmul: signed 8.8 fixed multiply with
// 32-bit-correct partial products on the hardware 16x16->low16 MUL.
// In: R0 = a, R1 = b. Out: R0 = (a*b) >> 8 (8.8). Clobbers R1-R3, R6, R7.
//
//	result = (ah*bh)<<8 + ah*bl + al*bh + (al*bl)>>8   (on |a|,|b|)
func (cg *CodeGenerator) emitFixmulHelper() {
	cg.functionAddrs["__fixmul"] = cg.builder.GetCodeLength()

	// sign = (a ^ b) >> 15  -> R3 (kept live; no calls below)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 3, 0))
	cg.builder.AddInstruction(rom.EncodeXOR(0, 3, 1))
	cg.hShrImm(3, 15)

	// a = abs(a)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 0))
	cg.hShrImm(6, 15)
	cg.hCmpImm(6, 0)
	aPos := cg.hBranch(rom.EncodeBEQ())
	cg.hMovImm(6, 0)
	cg.builder.AddInstruction(rom.EncodeSUB(0, 6, 0))
	cg.builder.AddInstruction(rom.EncodeMOV(0, 0, 6))
	cg.hPatchToHere(aPos)

	// b = abs(b)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 1))
	cg.hShrImm(6, 15)
	cg.hCmpImm(6, 0)
	bPos := cg.hBranch(rom.EncodeBEQ())
	cg.hMovImm(6, 0)
	cg.builder.AddInstruction(rom.EncodeSUB(0, 6, 1))
	cg.builder.AddInstruction(rom.EncodeMOV(0, 1, 6))
	cg.hPatchToHere(bPos)

	// R2 = (al*bl) >> 8
	cg.builder.AddInstruction(rom.EncodeMOV(0, 2, 0))
	cg.hAndImm(2, 0x00FF)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 1))
	cg.hAndImm(6, 0x00FF)
	cg.builder.AddInstruction(rom.EncodeMUL(0, 2, 6))
	cg.hShrImm(2, 8)

	// R2 += al*bh
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 0))
	cg.hAndImm(6, 0x00FF)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
	cg.hShrImm(7, 8)
	cg.builder.AddInstruction(rom.EncodeMUL(0, 6, 7))
	cg.builder.AddInstruction(rom.EncodeADD(0, 2, 6))

	// R2 += ah*bl
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 0))
	cg.hShrImm(6, 8)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
	cg.hAndImm(7, 0x00FF)
	cg.builder.AddInstruction(rom.EncodeMUL(0, 6, 7))
	cg.builder.AddInstruction(rom.EncodeADD(0, 2, 6))

	// R2 += (ah*bh) << 8
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 0))
	cg.hShrImm(6, 8)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 7, 1))
	cg.hShrImm(7, 8)
	cg.builder.AddInstruction(rom.EncodeMUL(0, 6, 7))
	cg.hShlImm(6, 8)
	cg.builder.AddInstruction(rom.EncodeADD(0, 2, 6))

	// apply sign
	cg.hCmpImm(3, 0)
	pos := cg.hBranch(rom.EncodeBEQ())
	cg.hMovImm(6, 0)
	cg.builder.AddInstruction(rom.EncodeSUB(0, 6, 2))
	cg.builder.AddInstruction(rom.EncodeMOV(0, 0, 6))
	cg.builder.AddInstruction(rom.EncodeRET())
	cg.hPatchToHere(pos)
	cg.builder.AddInstruction(rom.EncodeMOV(0, 0, 2))
	cg.builder.AddInstruction(rom.EncodeRET())
}

// storeIOByte writes the low byte of valueReg to an I/O address (8-bit MMIO).
func (cg *CodeGenerator) storeIOByte(addr uint16, valueReg uint8) {
	cg.hMovImm(7, addr)
	cg.builder.AddInstruction(rom.EncodeMOV(7, 7, valueReg)) // MOV [R7], Rvalue (8-bit store)
}

// buttonMasks maps CoreLX button names to their controller bitmasks
// (bit position per internal/input: UP=0..Z=11).
var buttonMasks = map[string]int64{
	"UP": 0x0001, "DOWN": 0x0002, "LEFT": 0x0004, "RIGHT": 0x0008,
	"A": 0x0010, "B": 0x0020, "X": 0x0040, "Y": 0x0080,
	"L": 0x0100, "R": 0x0200, "START": 0x0400, "Z": 0x0800,
}

// selectMatrixPlane writes the channel to the plane-select register (0x8080)
// so subsequent matrix-plane register writes target that plane.
func (cg *CodeGenerator) selectMatrixPlane(channel Expr) error {
	if err := cg.generateExpr(channel, 0); err != nil {
		return err
	}
	cg.storeIOByte(0x8080, 0)
	return nil
}

// writePlaneByte evaluates expr and writes its low byte to an 8-bit register.
func (cg *CodeGenerator) writePlaneByte(expr Expr, addr uint16) error {
	if err := cg.generateExpr(expr, 0); err != nil {
		return err
	}
	cg.storeIOByte(addr, 0)
	return nil
}

// writePlaneReg16 evaluates expr and writes it as a low/high byte pair to two
// consecutive 8-bit registers (addrLow, addrLow+1). Matrix-plane 16-bit
// registers are exposed as adjacent 8-bit I/O ports, so a single 16-bit store
// would only land the low byte; this writes both halves explicitly.
func (cg *CodeGenerator) writePlaneReg16(expr Expr, addrLow uint16) error {
	if err := cg.generateExpr(expr, 0); err != nil {
		return err
	}
	cg.storeIOByte(addrLow, 0)             // low byte
	cg.builder.AddInstruction(rom.EncodeMOV(0, 6, 0)) // R6 = value
	cg.hShrImm(6, 8)                       // R6 = value >> 8
	cg.storeIOByte(addrLow+1, 6)           // high byte
	return nil
}
