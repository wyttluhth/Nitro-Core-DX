# CoreLX Language Documentation

**For Nitro Core DX — documents the current shipping compiler**

> **Scope note (2026-06-12):** This is the reference for the compiler as
> implemented today in `internal/corelx`. The CoreLX **v1 language** is
> specified separately and is being built now:
> [CORELX_SYNTAX_V1.md](specifications/CORELX_SYNTAX_V1.md) (syntax charter),
> [CORELX_CARTRIDGE_FORMAT.md](specifications/CORELX_CARTRIDGE_FORMAT.md)
> (cartridge format), and the decision record in
> [Games/NitroPackInDemo/CORELX_EXTRACTION.md](../Games/NitroPackInDemo/CORELX_EXTRACTION.md).
> Where this document and the v1 charter differ (e.g. `ASSET_` prefixes,
> C-style `for`, the `&` operator), the charter is the destination; this
> document tracks what compiles today.

> **CoreLX** (pronounced *core elix*) is the native compiled programming language for the **Nitro Core DX** console.  
> CoreLX is a **compiled-only**, **hardware-first** language with **no interpreter**, **no virtual machine**, and **no runtime scripting layer**.  
> Each CoreLX source file produces **one ROM image** that runs directly on the Nitro Core DX emulator or future hardware.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Language Overview](#language-overview)
3. [Syntax Basics](#syntax-basics)
4. [Types](#types)
5. [Variables and Assignment](#variables-and-assignment)
6. [Control Flow](#control-flow)
7. [Functions](#functions)
8. [Structs](#structs)
9. [Assets](#assets)
10. [Sprites and OAM](#sprites-and-oam)
11. [Audio (APU)](#audio-apu)
12. [Built-in Functions Reference](#built-in-functions-reference)
13. [Examples](#examples)
14. [Test ROMs](#test-roms)
15. [Compiler Status](#compiler-status)
16. [Testing](#testing)

---

## Quick Start

### Installation

The CoreLX compiler is built with the project:

```bash
go build ./cmd/corelx
```

### Compile Your First Program

Create `hello.corelx`:

```corelx
function Start()
    ppu.enable_display()
    
    while true
        wait_vblank()
```

Compile it:

```bash
./corelx hello.corelx hello.rom
```

Run it:

```bash
./nitro-core-dx hello.rom
```

---

## Language Overview

### Core Principles

CoreLX is designed to feel:
- **Magical on the surface** - Simple, expressive syntax
- **Strict underneath** - Type-safe, compile-time checked
- **Powerful without being vague** - Direct hardware access
- **Learnable without dumbing anything down** - Clear, precise documentation

### Key Features

- **Indentation-based syntax** - No braces, no semicolons
- **Compiled to machine code** - Direct Nitro Core DX execution
- **Hardware-accurate** - Direct access to PPU, APU, OAM, VRAM
- **Single-file compilation** - One `.corelx` file = one ROM
- **Inline assets** - Embed graphics and data directly in source

---

## Syntax Basics

### Indentation

CoreLX uses indentation to define blocks (like Python):

```corelx
if x > 5
    y = 10
    z = 20
else
    y = 0
```

### Comments

Single-line comments start with `--`:

```corelx
-- This is a comment
x := 10  -- Inline comment
```

### Identifiers

- Start with letter or underscore
- Can contain letters, numbers, underscores
- Case-sensitive

---

## Types

### Built-in Types

- **Integers**: `i8`, `u8`, `i16`, `u16`, `i32`, `u32`
- **Boolean**: `bool`
- **Fixed Point**: `fx8_8`, `fx16_16`
- **Pointers**: `*T` (e.g., `*Sprite`, `*u8`)

### Type Inference

Use `:=` for type inference:

```corelx
x := 10        -- Inferred as i32
y := 0x1234    -- Inferred as i32
flag := true   -- Inferred as bool
```

### Explicit Types

Use `:` for explicit typing:

```corelx
x: u8 = 10
y: i16 = -100
ptr: *Sprite = &sprite
```

---

## Variables and Assignment

### Variable Declaration

```corelx
-- Inferred type
x := 10
name := "Hello"

-- Explicit type
count: u8 = 0
position: i16 = 100
```

### Assignment

```corelx
x = 20
sprite.tile = 5
position = position + 1
```

---

## Control Flow

### If Statements

```corelx
if x > 5
    y = 10
else
    y = 0
```

### Else If

```corelx
if x < 5
    y = 1
else
    if x < 10
        y = 2
    else
        y = 3
```

### While Loops

```corelx
i := 0
while i < 10
    -- Do something
    i = i + 1
```

### For Loops

```corelx
-- For loops are syntactic sugar for while loops
for i := 0; i < 10; i = i + 1
    -- Do something
```

---

## Functions

### Function Declaration

Currently, only the `Start()` function is required:

```corelx
function Start()
    -- Your game code here
```

User-defined functions are planned for a future release.

---

## Structs

### Struct Definition

```corelx
type Vec2 = struct
    x: i16
    y: i16

type Sprite = struct
    x_lo: u8
    x_hi: u8
    y: u8
    tile: u8
    attr: u8
    ctrl: u8
```

### Struct Initialization

```corelx
pos := Vec2()
pos.x = 100
pos.y = 200

hero := Sprite()
hero.tile = 0
hero.attr = SPR_PAL(1)
hero.ctrl = SPR_ENABLE()
```

---

## Assets

### Asset declaration for sprites

- **`tiles8`** — One 8×8 tile (32 bytes). Use for 8×8 sprites.
- **`tiles16`** — One 16×16 tile (128 bytes). Use for 16×16 sprites.
- **`tileset`** — Contiguous tile data (e.g. 128 bytes for one 16×16 “tile”). For a 128-byte tileset, the compiler uses the same VRAM stride as 16×16 sprites (base × 128).

```corelx
asset MyTiles: tiles8
    hex
        FF FF FF FF FF FF FF FF
        00 00 00 00 00 00 00 00

asset Ship: tileset
    hex
        -- 16 rows × 8 bytes = 128 bytes for one 16×16
        ...
```

### Loading tiles into VRAM

```corelx
base := gfx.load_tiles(ASSET_MyTiles, 0)
```

The second argument is the **tile index** (VRAM slot). The compiler chooses the correct stride (32 for 8×8, 128 for 16×16/128-byte tileset). To avoid the background repeating your sprite art, load sprites at **non-zero** indices (e.g. 16, 17) so BG tile 0 stays separate.

**Note**: The first argument can be an `ASSET_*` literal or a runtime `u16` asset ID for a declared asset. With an `ASSET_*` literal, tile data is inlined at compile time.

---

## Sprites and OAM

### Creating and configuring sprites

Set position with `sprite.set_pos`; set `tile` (from `gfx.load_tiles`), `attr` (palette + priority), and `ctrl` (enable + size). Color index 0 is transparent.

```corelx
hero := Sprite()
sprite.set_pos(&hero, 160, 100)
hero.tile = base
hero.attr = SPR_PAL(1) | SPR_PRI(0)
hero.ctrl = SPR_ENABLE() | SPR_SIZE_16()
```

### Writing to OAM

Each sprite uses an OAM slot (0, 1, 2, …). Write all visible sprites, then flush once per frame.

```corelx
oam.write(0, &hero)
oam.write(1, &enemy)
oam.flush()
```

### Sprite helpers

```corelx
SPR_PAL(n)         -- Palette bank 0-15 (lower 4 bits of attr)
SPR_PRI(n)         -- Priority 0-3
SPR_HFLIP()        -- Horizontal flip
SPR_VFLIP()        -- Vertical flip
SPR_ENABLE()       -- Enable sprite
SPR_SIZE_8()       -- 8×8 size
SPR_SIZE_16()      -- 16×16 size
SPR_BLEND(n)       -- Blend mode
SPR_ALPHA(n)       -- Alpha value
```

For setup order, tile indices, palettes, and multiple sprites, see **docs/guides/PROGRAMMING_GUIDE.md** → “Working with Sprites (Real-World Guide)” and the example `Games/SpriteProbe/ship.corelx`.

---

## Audio (APU)

Note: CoreLX currently exposes the legacy 4-channel APU built-ins documented below. The FM extension exists in the emulator/APU (`0x9100-0x91FF`) but does not yet have stable CoreLX language-level APIs. Current cgo-backed entrypoints default `NCDX_YM_BACKEND` to `ymfm`, and the V1 release target is YM2608/OPNA.

### Enabling APU

```corelx
apu.enable()
```

### Configuring Channels

```corelx
-- Set waveform (0=Sine, 1=Square, 2=Saw, 3=Noise)
apu.set_channel_wave(0, 1)

-- Set frequency (Hz)
apu.set_channel_freq(0, 440)

-- Set volume (0-255)
apu.set_channel_volume(0, 128)

-- Start playback
apu.note_on(0)

-- Stop playback
apu.note_off(0)
```

---

## Built-in Functions Reference

### Frame Synchronization

- `wait_vblank()` - Wait for VBlank period
- `frame_counter() -> u32` - Get current frame number

### Graphics

- `ppu.enable_display()` - Enable PPU display
- `gfx.load_tiles(asset, base) -> u16` - Load tile asset into VRAM at tile index `base`; returns `base`. Stride is 32 for 8×8, 128 for 16×16/128-byte tileset. Use non-zero `base` for sprites to avoid BG tile 0.
- `bg.enable(layer)` - Enable a background layer
- `bg.disable(layer)` - Disable a background layer
- `bg.set_scroll(layer, x, y)` - Set per-layer scroll offsets
- `bg.set_priority(layer, priority)` - Set explicit layer priority (0-3)
- `bg.set_tile_size(layer, size)` - Set tile size (`8` or `16`)
- `bg.set_tilemap_base(layer, base)` - Set the layer tilemap base address
- `bg.load_tilemap(asset, layer) -> u16` - Load a packed tilemap asset into the layer's configured tilemap base; returns the base used
- `bg.set_source_mode(layer, mode)` - Select source mode (`0=tilemap`, `1=bitmap/reserved`)
- `bg.bind_transform(layer, channel)` - Bind a layer to transform channel `0-3`
- `bg.set_tile(layer, x, y, tile, attr)` - Write one tilemap entry at tile coordinates `x, y`
- `bg.fill_span(layer, x, y, count, tile, attr)` - Fill `count` tilemap entries on one row
- `bg.clear(layer, tile, attr)` - Clear the full 32×32 tilemap with one tile/attribute pair
- `raster.enable(table_base, layer_mask, rebind, priority, tilemap_base, source_mode)` - Enable scanline-command mode and program the active table/control registers
- `raster.disable()` - Disable scanline-command mode
- `raster.set_scanline_scroll(scanline, layer, x, y)` - Write one scanline scroll payload entry for a layer
- `raster.set_scanline_matrix(scanline, layer, a, b, c, d)` - Write one scanline affine matrix payload entry for a layer
- `raster.set_scanline_center(scanline, layer, x, y)` - Write one scanline transform center/origin entry for a layer
- `raster.set_scanline_rebind(scanline, layer, channel)` - Write one scanline transform-channel rebinding entry when rebind tables are enabled
- `raster.set_scanline_priority(scanline, layer, priority)` - Write one scanline layer-priority entry when priority tables are enabled
- `raster.set_scanline_tilemap_base(scanline, layer, base)` - Write one scanline tilemap-base override when tilemap-base tables are enabled
- `raster.set_scanline_source_mode(scanline, layer, mode)` - Write one scanline source-mode entry when source-mode tables are enabled

### Matrix Mode

- `matrix.bind(layer, channel)` - Bind a layer to a transform channel
- `matrix.enable(layer)` - Enable the bound transform channel for that layer
- `matrix.disable(layer)` - Disable the bound transform channel for that layer
- `matrix.set_matrix(layer, a, b, c, d)` - Set the 2x2 affine matrix (8.8 fixed point)
- `matrix.set_center(layer, x, y)` - Set transform center/origin
- `matrix.identity(layer)` - Reset matrix to identity (`A=1.0`, `B=0`, `C=0`, `D=1.0`)
- `matrix.set_flags(layer, mirror_h, mirror_v, outside_mode, direct_color)` - Set matrix control flags while preserving enable state

### Matrix Planes

- `matrix_plane.enable(channel, size)` - Enable dedicated matrix plane `0-3` with size `32`, `64`, or `128` tiles per side
- `matrix_plane.disable(channel)` - Disable the dedicated matrix plane while preserving its programmed size bits
- `matrix_plane.load_tiles(asset, channel, base) -> u16` - Load a `tiles8`/`tiles16`/`sprite`/`tileset` asset into the dedicated pattern memory of matrix plane `channel` at tile index `base`; returns `base`
- `matrix_plane.load_tilemap(asset, channel) -> u16` - Load a packed tilemap asset into the dedicated matrix plane tilemap memory for `channel`; returns `channel`
- `matrix_plane.set_tile(channel, x, y, tile, attr)` - Write one tilemap entry into the dedicated matrix plane tilemap
- `matrix_plane.fill_rect(channel, x, y, w, h, tile, attr)` - Fill a rectangle in the dedicated matrix plane tilemap
- `matrix_plane.clear(channel, tile, attr)` - Clear the full dedicated matrix plane tilemap using one tile/attribute pair

Matrix planes are separate from ordinary BG VRAM tilemaps. To render one, bind a visible layer to the same transform channel with `bg.bind_transform(...)`, enable Matrix Mode on that layer with `matrix.enable(...)`, and then populate the plane's dedicated tilemap/pattern memory through `matrix_plane.*`.

Simple setup example:

```corelx
function Start()
    ppu.enable_display()
    bg.enable(0)
    bg.bind_transform(0, 0)
    bg.set_tilemap_base(0, 0x4000)
    bg.set_priority(0, 2)

    matrix.enable(0)
    matrix.identity(0)
    matrix.set_flags(0, false, false, 0, false)
    matrix.set_center(0, 160, 100)
    matrix.set_matrix(0, 0x0100, 0, 0, 0x0100)

    while true
        wait_vblank()
```

Simple tilemap-asset workflow:

```corelx
asset MapA: tilemap hex
    00 00 01 00

function Start()
    ppu.enable_display()
    bg.enable(0)
    bg.set_tilemap_base(0, 0x4000)
    bg.load_tilemap(ASSET_MapA, 0)

    while true
        wait_vblank()
```

Minimal raster workflow:

```corelx
function Start()
    ppu.enable_display()
    bg.enable(0)
    bg.bind_transform(0, 0)
    matrix.enable(0)
    matrix.identity(0)

    raster.enable(0x3000, 0x01, false, false, true, false)

    scan := 0
    while scan < 100
        raster.set_scanline_scroll(scan, 0, 0, 0)
        raster.set_scanline_matrix(scan, 0, 0x0100, 0, 0, 0x0100)
        raster.set_scanline_center(scan, 0, 0, 0)
        raster.set_scanline_tilemap_base(scan, 0, 0x1000)
        scan = scan + 1

    while true
        wait_vblank()
```

Minimal dedicated matrix-plane workflow:

```corelx
asset Floor: tiles8 hex
    11 11 11 11 11 11 11 11
    11 11 11 11 11 11 11 11
    11 11 11 11 11 11 11 11
    11 11 11 11 11 11 11 11

function Start()
    gfx.init_default_palettes()
    bg.enable(0)
    bg.bind_transform(0, 0)

    matrix.enable(0)
    matrix.identity(0)
    matrix.set_center(0, 160, 100)

    matrix_plane.enable(0, 128)
    matrix_plane.load_tiles(ASSET_Floor, 0, 0)
    matrix_plane.clear(0, 0, 0)
    matrix_plane.fill_rect(0, 32, 0, 32, 128, 0, 0)

    ppu.enable_display()
    while true
        wait_vblank()
```

Reference demo ROM:

- [`test/roms/raster_showcase.corelx`](/home/aj/Documents/Development/Nitro-Core-DX/test/roms/raster_showcase.corelx)
- [`test/roms/graphics_image_demo.corelx`](/home/aj/Documents/Development/Nitro-Core-DX/test/roms/graphics_image_demo.corelx)
- [`test/roms/matrix_plane_showcase.corelx`](/home/aj/Documents/Development/Nitro-Core-DX/test/roms/matrix_plane_showcase.corelx)
- [`test/roms/matrix_plane_pipeline_showcase.corelx`](/home/aj/Documents/Development/Nitro-Core-DX/test/roms/matrix_plane_pipeline_showcase.corelx)

### Sprites

- `sprite.set_pos(sprite, x, y)` - Set sprite position
- `oam.write(index, sprite)` - Write sprite to OAM
- `oam.flush()` - Flush OAM writes

### Audio

- `apu.enable()` - Enable APU
- `apu.set_channel_wave(ch, wave)` - Set waveform
- `apu.set_channel_freq(ch, freq)` - Set frequency
- `apu.set_channel_volume(ch, vol)` - Set volume
- `apu.note_on(ch)` - Start note
- `apu.note_off(ch)` - Stop note
- `fm.*` APIs - Planned (FM extension exists at hardware/MMIO level; CoreLX API integration is not finalized yet)

### Input

- `input.read(controller) -> u16` - Read controller state

### Memory

- `mem.read(addr) -> u8` - Read one byte from CPU-visible memory/MMIO and return it zero-extended in the destination register
- `mem.write(addr, value)` - Write the low byte of `value` to CPU-visible memory/MMIO

`mem.read` / `mem.write` are the current CoreLX-safe path for byte-addressed
MMIO work. If you need explicit 16-bit memory traffic today, use assembly or a
purpose-built higher-level helper instead of assuming these built-ins perform
word reads/writes.

### Sprite Helpers

- `SPR_PAL(p) -> u8` - Palette bits
- `SPR_PRI(p) -> u8` - Priority bits
- `SPR_HFLIP() -> u8` - Horizontal flip
- `SPR_VFLIP() -> u8` - Vertical flip
- `SPR_ENABLE() -> u8` - Enable bit
- `SPR_SIZE_8() -> u8` - 8×8 size
- `SPR_SIZE_16() -> u8` - 16×16 size
- `SPR_BLEND(mode) -> u8` - Blend mode
- `SPR_ALPHA(a) -> u8` - Alpha value

---

## Compiler Status

### ✅ Fully Implemented

- Lexer (tokenization)
- Parser (AST construction)
- Semantic analyzer (type checking)
- Code generator (machine code)
- ROM builder
- All built-in functions
- Control flow (if, while, for)
- User-defined functions (with parameters and return values)
- Struct initialization
- Variable declarations
- Expression evaluation

### 🚧 In Progress

- Variable storage optimization (basic implementation works, but can be improved)

### Asset Build Notes (Current)

- In-source `asset ...` declarations are compiled through the normal build path.
- The compiler service path also supports external asset manifests via `corelx.assets.json` (loaded from the source directory when present).
- Dev Kit lab tools are source-edit helpers; compiler outputs (manifest/bundle/ROM) are the authoritative build result.

### 📋 Planned (delivered by the v1 charter work)

- Array support
- Global variables, constants, `fixed` type, string codegen
- Enhanced expression optimization

(User-defined functions are implemented — see Fully Implemented above.)

---

## Testing

### Running Tests

```bash
go test ./internal/corelx/...
```

### Test ROMs

Test ROMs are in `test/roms/`:
- `simple_test.corelx` - Basic features
- `example.corelx` - Simple game loop
- `full_example.corelx` - Complete sprite example
- `corelx_comprehensive_test.corelx` - All language features

---

## Examples

### Example 1: Simple Game Loop

Basic frame synchronization:

```corelx
function Start()
    ppu.enable_display()
    
    frame := 0
    while true
        wait_vblank()
        frame = frame + 1
```

**Source**: `test/roms/example.corelx`

---

### Example 2: Sprite with Asset

Complete sprite example with asset loading:

```corelx
asset HeroTiles: tiles8
    hex
        00 00 11 11 22 22 33 33
        44 44 55 55 66 66 77 77

function Start()
    ppu.enable_display()

    base := gfx.load_tiles(ASSET_HeroTiles, 0)

    hero := Sprite()
    sprite.set_pos(&hero, 120, 80)
    hero.tile = base
    hero.attr = SPR_PAL(1) | SPR_PRI(2)
    hero.ctrl = SPR_ENABLE() | SPR_SIZE_16()

    while true
        wait_vblank()
        oam.write(0, &hero)
        oam.flush()
```

**Source**: `test/roms/full_example.corelx`

**Features demonstrated**:
- Asset declaration
- Sprite initialization
- Struct member access
- Sprite helpers (`SPR_PAL`, `SPR_PRI`, `SPR_ENABLE`, `SPR_SIZE_16`)
- OAM operations

---

### Example 3: Audio Playback

Simple audio example:

```corelx
function Start()
    apu.enable()
    apu.set_channel_wave(0, 1)  -- Square wave
    apu.set_channel_freq(0, 440)  -- A4 note
    apu.set_channel_volume(0, 128)
    apu.note_on(0)
    
    while true
        wait_vblank()
```

**Features demonstrated**:
- APU initialization
- Channel configuration
- Waveform selection
- Frequency and volume control
- Note playback

---

### Example 4: Input and Movement

Reading input and moving a sprite:

```corelx
function Start()
    ppu.enable_display()
    
    player := Sprite()
    player_x := 160
    player_y := 100
    sprite.set_pos(&player, player_x, player_y)
    player.tile = 0
    player.attr = SPR_PAL(0)
    player.ctrl = SPR_ENABLE()
    
    while true
        wait_vblank()
        
        -- Read input
        buttons := input.read(0)
        
        -- Move player
        if (buttons & 0x01) != 0  -- UP
            if player_y > 8
                player_y = player_y - 2
        if (buttons & 0x02) != 0  -- DOWN
            if player_y < 192
                player_y = player_y + 2
        if (buttons & 0x04) != 0  -- LEFT
            if player_x > 8
                player_x = player_x - 2
        if (buttons & 0x08) != 0  -- RIGHT
            if player_x < 312
                player_x = player_x + 2
        
        sprite.set_pos(&player, player_x, player_y)
        oam.write(0, &player)
        oam.flush()
```

**Features demonstrated**:
- Input reading
- Bitwise operations
- Conditional movement
- Boundary checking
- Sprite position updates

**Source**: Based on `test/roms/sprite_eater_game.corelx`

---

### Example 5: Comprehensive Feature Test

This example demonstrates all CoreLX language features:

```corelx
-- See test/roms/corelx_comprehensive_test.corelx for complete example

function Start()
    -- Variable declarations
    x := 10
    y: u8 = 20
    
    -- Control flow
    if x > 5
        y = y + 1
    else
        y = y - 1
    
    -- Loops
    i := 0
    while i < 10
        i = i + 1
    
    -- Structs
    hero := Sprite()
    hero.tile = 0
    hero.attr = SPR_PAL(1)
    
    -- Expressions
    sum := x + y
    flag := x == 10
    bitwise := 0x0F & 0xF0
```

**Source**: `test/roms/corelx_comprehensive_test.corelx`

---

## Test ROMs

The `test/roms/` directory contains several example ROMs:

- **`example.corelx`** - Simple game loop
- **`full_example.corelx`** - Complete sprite example
- **`sprite_eater_game.corelx`** - Full game with input, collision, and multiple sprites
- **`corelx_comprehensive_test.corelx`** - Tests all language features
- **`apu_test.corelx`** - Audio function tests

To compile and run:

```bash
# Compile
./corelx test/roms/example.corelx roms/example.rom

# Run
./nitro-core-dx roms/example.rom
```

See [test/roms/README_TEST_ROMS.md](../test/roms/README_TEST_ROMS.md) for more details.

---

## Additional Resources

- [Debugging Guide](DEBUGGING_GUIDE.md) - How to debug CoreLX programs
- [Programming Manual](../PROGRAMMING_MANUAL.md) - Complete guide covering both CoreLX and Assembly
- [CoreLX v1 decision record](../Games/NitroPackInDemo/CORELX_EXTRACTION.md) - Design decisions and rationale
- [Compiler Implementation](archive/corelx/) - Historical implementation notes

---

## See Also

- **New to Nitro Core DX?** Start with the [Programming Manual](../PROGRAMMING_MANUAL.md) for a comprehensive introduction
- **Need Assembly?** See the [Programming Manual](../PROGRAMMING_MANUAL.md) for assembly language details
- **Debugging Issues?** Check the [Debugging Guide](DEBUGGING_GUIDE.md)
- **Want to understand design decisions?** Read the [CoreLX v1 decision record](../Games/NitroPackInDemo/CORELX_EXTRACTION.md)

---

**Last Updated**: January 29, 2026
