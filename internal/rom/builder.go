package rom

import (
	"encoding/binary"
	"fmt"
	"os"
)

// ROMBuilder helps build ROM files
type ROMBuilder struct {
	code []uint16

	// Optional read-only data region placed in higher ROM banks (for DMA
	// sources like bitmap images). Starts at dataStartBank, offset 0x8000.
	dataStartBank uint8
	dataRegion    []byte
}

// SetDataRegion places a contiguous read-only data blob starting at the given
// ROM bank (offset 0x8000), for use as a DMA source. The code must fit below
// dataStartBank.
func (b *ROMBuilder) SetDataRegion(startBank uint8, data []byte) {
	b.dataStartBank = startBank
	b.dataRegion = data
}

// NewROMBuilder creates a new ROM builder
func NewROMBuilder() *ROMBuilder {
	return &ROMBuilder{
		code: make([]uint16, 0),
	}
}

// AddInstruction adds an instruction word
func (b *ROMBuilder) AddInstruction(instruction uint16) {
	b.code = append(b.code, instruction)
}

// AddImmediate adds an immediate value (for instructions that need it)
func (b *ROMBuilder) AddImmediate(value uint16) {
	b.code = append(b.code, value)
}

// SetImmediateAt sets an immediate value at a specific word index
// Useful for patching branch offsets after the target address is known
func (b *ROMBuilder) SetImmediateAt(wordIndex int, value uint16) {
	if wordIndex < 0 || wordIndex >= len(b.code) {
		panic(fmt.Sprintf("SetImmediateAt: index %d out of range (code length: %d)", wordIndex, len(b.code)))
	}
	b.code[wordIndex] = value
}

// GetCodeLength returns the current code length in words
func (b *ROMBuilder) GetCodeLength() int {
	return len(b.code)
}

// BuildROM builds the ROM file
func (b *ROMBuilder) BuildROM(entryBank uint8, entryOffset uint16, outputPath string) error {
	romData, err := b.BuildROMBytes(entryBank, entryOffset)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, romData, 0644)
}

// BuildROMBytes builds the ROM image and returns the bytes without writing to disk.
func (b *ROMBuilder) BuildROMBytes(entryBank uint8, entryOffset uint16) ([]byte, error) {
	codeBytes := uint32(len(b.code) * 2)

	// Total ROM size must cover both the code and any high-bank data region.
	romSize := codeBytes
	if len(b.dataRegion) > 0 {
		dataStart := uint32(b.dataStartBank-1) * uint32(ROMBankSizeBytes)
		// The code must not spill into the data banks.
		if codeBytes > dataStart {
			return nil, fmt.Errorf("code (%d bytes) overflows into the data region starting at bank %d (0x%X)", codeBytes, b.dataStartBank, dataStart)
		}
		dataEnd := dataStart + uint32(len(b.dataRegion))
		if dataEnd > romSize {
			romSize = dataEnd
		}
	}

	// Create ROM data
	romData := make([]byte, 32+romSize)

	// Write header
	// Magic: "RMCF" = 0x46434D52
	binary.LittleEndian.PutUint32(romData[0:4], 0x46434D52)
	// Version: 1
	binary.LittleEndian.PutUint16(romData[4:6], 1)
	// ROM Size
	binary.LittleEndian.PutUint32(romData[6:10], romSize)
	// Entry Bank
	binary.LittleEndian.PutUint16(romData[10:12], uint16(entryBank))
	// Entry Offset
	binary.LittleEndian.PutUint16(romData[12:14], entryOffset)
	// Mapper Flags: 0 (LoROM)
	binary.LittleEndian.PutUint16(romData[14:16], 0)
	// Checksum: 0 (unused)
	binary.LittleEndian.PutUint32(romData[16:20], 0)
	// Reserved: 0
	for i := 20; i < 32; i++ {
		romData[i] = 0
	}

	// Write code (little-endian)
	for i, word := range b.code {
		offset := 32 + (i * 2)
		binary.LittleEndian.PutUint16(romData[offset:offset+2], word)
	}

	// Write the high-bank data region (DMA source), if any.
	if len(b.dataRegion) > 0 {
		base := 32 + uint32(b.dataStartBank-1)*uint32(ROMBankSizeBytes)
		copy(romData[base:base+uint32(len(b.dataRegion))], b.dataRegion)
	}

	return romData, nil
}

// Helper functions for instruction encoding

// EncodeMOV encodes a MOV instruction
func EncodeMOV(mode, reg1, reg2 uint8) uint16 {
	return 0x1000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeADD encodes an ADD instruction
func EncodeADD(mode, reg1, reg2 uint8) uint16 {
	return 0x2000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeSUB encodes a SUB instruction
func EncodeSUB(mode, reg1, reg2 uint8) uint16 {
	return 0x3000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeAND encodes an AND instruction
func EncodeAND(mode, reg1, reg2 uint8) uint16 {
	return 0x6000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeOR encodes an OR instruction
func EncodeOR(mode, reg1, reg2 uint8) uint16 {
	return 0x7000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeXOR encodes an XOR instruction
func EncodeXOR(mode, reg1, reg2 uint8) uint16 {
	return 0x8000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeCMP encodes a CMP instruction
func EncodeCMP(mode, reg1, reg2 uint8) uint16 {
	return 0xC000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeBEQ encodes a BEQ instruction
func EncodeBEQ() uint16 {
	return 0xC100
}

// EncodeBNE encodes a BNE instruction
func EncodeBNE() uint16 {
	return 0xC200
}

// EncodeBGT encodes a BGT instruction
func EncodeBGT() uint16 {
	return 0xC300
}

// EncodeBLT encodes a BLT instruction
func EncodeBLT() uint16 {
	return 0xC400
}

// EncodeBGE encodes a BGE instruction
func EncodeBGE() uint16 {
	return 0xC500
}

// EncodeBLE encodes a BLE instruction
func EncodeBLE() uint16 {
	return 0xC600
}

// EncodeSHL encodes a SHL instruction
func EncodeSHL(mode, reg1, reg2 uint8) uint16 {
	return 0xA000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeSHR encodes a SHR instruction
func EncodeSHR(mode, reg1, reg2 uint8) uint16 {
	return 0xB000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeJMP encodes a JMP instruction
func EncodeJMP() uint16 {
	return 0xD000
}

// EncodeCALL encodes a CALL instruction
func EncodeCALL() uint16 {
	return 0xE000
}

// EncodeRET encodes a RET instruction
func EncodeRET() uint16 {
	return 0xF000
}

// EncodeNOP encodes a NOP instruction
func EncodeNOP() uint16 {
	return 0x0000
}

// CalculateBranchOffset calculates a branch offset
// currentPC: PC pointing to the offset word (after branch instruction)
// targetPC: Target address to branch to
// Offset is relative to PC after instruction and offset word (currentPC + 2)
func CalculateBranchOffset(currentPC, targetPC uint16) int16 {
	// Offset is relative to PC after instruction and offset word
	// currentPC points to the offset word, so PC after offset word is currentPC + 2
	offset := int32(targetPC) - int32(currentPC) - 2
	if offset < -32768 || offset > 32767 {
		panic(fmt.Sprintf("branch offset out of range: %d (currentPC=0x%04X, targetPC=0x%04X)", offset, currentPC, targetPC))
	}
	return int16(offset)
}

// EncodeMUL encodes a MUL instruction (opcode 0x4).
// mode 0: MUL R1, R2; mode 1: MUL R1, #imm. Result: low 16 bits in R1.
func EncodeMUL(mode, reg1, reg2 uint8) uint16 {
	return 0x4000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}

// EncodeDIV encodes a DIV instruction (opcode 0x5).
// mode 0: DIV R1, R2; mode 1: DIV R1, #imm. Unsigned; div-by-zero sets FlagD.
func EncodeDIV(mode, reg1, reg2 uint8) uint16 {
	return 0x5000 | (uint16(mode) << 8) | (uint16(reg1) << 4) | uint16(reg2)
}
