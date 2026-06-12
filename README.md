# Nitro-Core-DX

**A Fantasy Console Emulator Combining SNES Graphics with Genesis Power**

A custom 16-bit fantasy console emulator inspired by classic 8/16-bit consoles, designed to combine the best features of the SNES and Sega Genesis into a single, powerful platform.

> **✅ Architecture Stable**: The core hardware architecture is complete and stable. The emulator and tooling are actively maintained, and tests/documentation continue to be refined as clock-driven behavior and debug tooling evolve.

> **Current Focus (2026-06-12)**: The `Games/NitroPackInDemo` ROM-first showcase is complete (title, overworld, interior, dialogue, credits), and the CoreLX v1 language design is settled. Active work is the M8 CoreLX rebuild: implementing the v1 language ([syntax charter](docs/specifications/CORELX_SYNTAX_V1.md), [cartridge format](docs/specifications/CORELX_CARTRIDGE_FORMAT.md), [decision record](Games/NitroPackInDemo/CORELX_EXTRACTION.md)) and rebuilding the demo in CoreLX to validate it. **Full CoreLX support is planned for v0.2.5, with full Dev Kit readiness targeted for v0.3.0.**

---

## Meet Nitro-Core-DX

Ever wonder what would happen if you took the SNES's gorgeous graphics and mixed them with the Genesis's raw horsepower? That's exactly what Nitro-Core-DX is all about. It's a fantasy console that doesn't just emulate the classics—it creates something entirely new by combining the best of both worlds.

Think of it as the console that could have existed in an alternate timeline where Nintendo and Sega decided to collaborate instead of compete. I'm building this from the ground up with modern tools, but with the soul of the 16-bit era.

---

## The Vision: Best of Both Worlds

Nitro-Core-DX started with a simple question: *"What if?"* What if you could take the SNES's beautiful graphics and combine them with the Genesis's raw speed? What if you didn't have to choose between Mode 7 effects and smooth 60 FPS gameplay?

This isn't just another emulator—it's a passion project that's building something genuinely new. I'm not trying to recreate history; I'm trying to create the console that *should have* existed. And I'm doing it the right way: cycle-accurate emulation, proper architecture, comprehensive testing, and documentation that actually makes sense.

### What I'm Stealing (Politely) from SNES

The SNES brought some incredible graphics tech, and Nitro-Core-DX brings all of it:

- **4 Background Layers** - Parallax scrolling that'll make your eyes happy
- **Matrix Mode** - Mode 7-style perspective and rotation (but better, because it can do it on multiple layers simultaneously)
- **32,768 Colors** - That gorgeous 15-bit RGB555 palette
- **Sprite Magic** - Priorities, blending modes, alpha transparency—the works
- **Smart Memory** - Banked architecture that gives you flexibility without headaches

### What I'm Borrowing from Genesis

The Genesis was fast, and I like fast:

- **~7.67 MHz CPU** - Nearly 3× faster than the SNES's 2.68 MHz
- **DMA That Actually Works** - Fast memory transfers that don't slow you down
- **Arcade Performance** - The kind of speed that makes racing games and shooters feel *right*

### The Result?

A fantasy console that gives you SNES-quality visuals running at Genesis-level performance. Target is smooth 60 FPS (now achieving steady 60 FPS on the current desktop emulator build) with complex graphics, advanced parallax scrolling, and Matrix Mode effects that can handle 3D landscapes and racing games.

**My Philosophy:**
I'm not in a rush. This is a long-term project where doing it right matters more than doing it fast. Every component gets the attention it deserves—from cycle-accurate CPU emulation to hardware-accurate synchronization signals. I'm building something that'll last.

---

## Why Go?

I didn't just pick Go because it's trendy. I evaluated multiple languages and Go won because it hits the sweet spot between "fast enough" and "actually maintainable."

Here's why Go works so well for Nitro-Core-DX:

- **Performance**: Target is 60 FPS (now achieving steady 60 FPS on the current desktop emulator build, with optimization ongoing for more headroom)
- **Developer Experience**: Clean syntax that doesn't make you want to throw your keyboard
- **Concurrency**: Built-in goroutines that make audio/rendering threading actually pleasant
- **Cross-Platform**: One binary, runs everywhere (Linux, macOS, Windows—you name it)
- **Memory Safety**: Garbage collected, but not in a "pause the world for 5 seconds" kind of way
- **Maintainability**: Code that you can actually read and understand six months later

The best part? When I eventually port this to FPGA hardware, the architecture I've built in Go will translate cleanly. That's not an accident—it's by design.

---

## Console Design

Here's what the console will look like when I build the first prototype:

<div align="center">

![Console Isometric View](Images/Console%20isometric.jpg)

*Isometric view of the Nitro-Core-DX console*

![Console Top View](Images/Console%20Top%20view.png)

*Top-down view showing the console design*

![Controller](Images/Controller.jpg)

*The Nitro-Core-DX controller design*

</div>

---

## Project Components

Nitro-Core-DX is a complete fantasy console system built from scratch, consisting of five major pieces that work together:

1. **Hardware Architecture** - Custom 16-bit CPU, memory map, PPU (graphics), APU (audio), and I/O systems
2. **Emulator** - Hardware-model-focused CPU/PPU/APU emulation with deterministic frame stepping and debug tooling
3. **CoreLX Compiler** - Custom compiled language with Lua-like syntax for hardware-first programming
4. **Nitro-Core-DX App (Dev Kit)** - Integrated editor/build/run environment with embedded emulator pane
5. **Assembler v1** - Text assembly (`.asm`) -> ROM pipeline for lower-level workflows

For detailed information about the development process and challenges, see [Development Notes](docs/DEVELOPMENT_NOTES.md).

---

## Current Showcase Demo

The current proving ground for Nitro-Core-DX is **NitroPackInDemo**, a ROM-first showcase and pack-in style demo that is being built before the higher-level CoreLX version. The idea is to finish the proof-of-concept in raw ROM form first, then use that working demo to shape the language, compiler, and Dev Kit workflows around a real game-sized target instead of toy snippets.

![NitroPackInDemo Screenshot](Resources/ShowcaseDemo.png)

Today, this showcase demo is where matrix-floor traversal, vertical projected facades, scene transitions, player movement, and future adventure/RPG-style building blocks are being proven. The current slice already includes a title flow, pseudo-3D overworld traversal, a visible player character, and an enterable building facade that is being tuned against the live matrix projection path.

Why this matters:

- It acts as the current end-to-end proof of concept for the emulator.
- It gives the Dev Kit a real pack-in demo target instead of isolated test cases.
- It will become the reference app used to design and validate the next stage of CoreLX ergonomics.

Current direction for the showcase:

- ROM-first implementation in `Games/NitroPackInDemo`
- pseudo-3D overworld traversal using matrix-floor rendering
- world-space building facade / billboard-style scene interaction
- later interior showcase room, NPC interaction, dialogue, and credits

Build and run the current showcase ROM locally:

```bash
go run -tags testrom_tools ./Games/NitroPackInDemo -out roms/nitro_pack_in_demo.rom
./nitro-core-dx -rom roms/nitro_pack_in_demo.rom
```

If you are using the integrated Dev Kit instead of the standalone emulator, you can also load `roms/nitro_pack_in_demo.rom` directly in the embedded emulator pane after generating it.

---

## Project Status

**Validation Snapshot (2026-02-28):**
- `go build -tags no_sdl_ttf -o nitro-core-dx ./cmd/emulator` passes locally.
- `go test -tags no_sdl_ttf ./... -timeout 180s` passes in a local environment without SDL2_ttf development libraries.
- Some ROM generator helper programs are intentionally gated behind the `testrom_tools` build tag to avoid multiple-`main` conflicts during normal test runs.

### ✅ Currently Implemented

- **Core Emulation**: CPU, Memory, PPU, APU, Input systems implemented and under active validation
- **Synchronization**: One-shot completion status, frame counter, VBlank flag
- **Graphics System**: Complete PPU with all features
  - Sprite system with priority, blending, and alpha transparency
  - 4 background layers with per-layer Matrix Mode transformations
  - Matrix Mode with outside-screen handling and direct color mode
  - Dedicated matrix-plane path with bitmap-backed planes, perspective row projection, and vertical projected quads
  - Mosaic effect, DMA transfers, sprite-to-background priority
- **Audio System**: 4-channel legacy audio synthesis with PCM playback support
  - Waveform generation (sine, square, saw, noise)
  - PCM sample playback with loop and one-shot modes
  - Volume control and duration management
  - FM extension host interface + YM2608-capable runtime backend path (with compatibility fallback controls)
- **Interrupt System**: Complete IRQ/NMI handling with vector table
- **ROM Loading**: Complete ROM header parsing and execution
- **Test Suite**: Broad regression coverage across CPU/PPU/APU/emulator paths (includes some long-running timing tests)
- **Assembly Toolchain v1**: text assembler (`.asm` -> `.rom`) for advanced low-level workflows
- **Nitro-Core-DX App (Dev Kit)**: Professional integrated development environment
  - Traditional IDE menu bar (File, Edit, View, Build, Debug, Tools, Help)
  - Domain-grouped toolbar (Project, Build, Run/Debug, View)
  - CoreLX editor with inline syntax highlighting, line numbers, active-line emphasis, and jump-to-diagnostic flow
  - Embedded hardware emulator with Build + Run workflow
  - Three view modes: Split View, Emulator Focus, Code Only
  - Project templates: Blank Game, Minimal Loop, Sprite Demo, Tilemap Demo, Shmup Starter, Matrix Mode Demo
  - Sprite Lab: pixel-art editor with 16 palette banks, RGB555 color editing, undo/redo, import/export, Insert/Apply-to-project asset flows, and transparent-index checker visualization
  - Tilemap Lab: tilemap paint/edit tool with tile+attribute entries, tile atlas picker, source tileset parsing, import/export, and Insert/Apply-to-project flows
  - Autosave with crash recovery
  - Settings persistence (view mode, split offsets, recent files, UI density)
  - Build state tracking (`Draft` -> `Validating...` -> `Validated` / `Error`) and diagnostics counters
  - UI density modes (Compact / Standard) for workspace efficiency
  - Native OS maximize/restore, resizable panels, layout presets
  - Load ROM for direct `.rom` testing without recompilation

### 🚧 In Progress

- **CoreLX v1 implementation (M8)**: language core (globals, constants, arrays, `fixed`, strings), image/sprite/background asset formats, projection/camera builtins, and the demo rebuilt in CoreLX as the acceptance test — per the [v1 syntax charter](docs/specifications/CORELX_SYNTAX_V1.md) and [decision record](Games/NitroPackInDemo/CORELX_EXTRACTION.md)
- **Nitro-Core-DX App Expansion**: Sound Studio, find/replace, richer editor UX polish
- **Audio Roadmap**: YM2608 conformance refinement, broader subsystem parity, and future Sound Studio-facing authoring flow

### 🗺️ Version Roadmap

- **v0.2.0 (current)** — pack-in demo complete; CoreLX v1 language fully designed
- **v0.2.5** — **full CoreLX support**: the v1 language implemented (syntax charter, single-file cartridge format, module system), validated by rebuilding the pack-in demo in CoreLX
- **v0.3.0** — **full Dev Kit readiness**: complete authoring workflow around the finished language (editors writing cartridge text sections, sprite/tilemap/sound tooling, rewritten programming manual)

### ❌ Optional Enhancements (Not Required)

- **Vertical Sprites**: 3D sprite scaling for Matrix Mode (can be added later)
- **FM Synthesis**: Current runtime uses a YM2608-capable backend path with ongoing conformance refinement; V1 release target remains YM2608

For detailed status and documentation navigation, see [docs/README.md](docs/README.md) and [docs/HARDWARE_FEATURES_STATUS.md](docs/HARDWARE_FEATURES_STATUS.md).

---

## System Specifications

| Feature | Specification |
|---------|--------------|
| **Display Resolution** | 320×200 pixels (landscape) / 200×320 (portrait) |
| **Color Depth** | 256 colors (8-bit indexed) |
| **Color Palette** | 256-color CGRAM (RGB555 format, 32,768 possible colors) |
| **Tile Size** | 8×8 or 16×16 pixels (configurable per layer) |
| **Max Sprites** | 128 sprites |
| **Background Layers** | 4 independent layers (BG0, BG1, BG2, BG3) |
| **Matrix Mode** | Mode 7-style effects with per-layer transforms, HDMA updates, outside-screen handling, and direct color |
| **Audio Channels** | 4 legacy channels + operational YM2608-capable FM backend path (with ongoing conformance refinement) |
| **Audio Sample Rate** | 44,100 Hz |
| **CPU Speed** | ~7.67 MHz (127,820 cycles per frame at 60 FPS, Genesis-like) |
| **Memory** | 64KB per bank, 256 banks (16MB total address space) |
| **ROM Size** | Up to 7.8MB (125 banks × 32KB LoROM windows) |
| **Frame Rate** | Target: 60 FPS (Currently: steady 60 FPS on current desktop build) |

### Performance Targets

- **Target: 60 FPS** - Goal is steady frame rate with no drops
- **Current: Steady 60 FPS** - Currently holding 60 FPS on the current desktop emulator build; optimization work continues for headroom and consistency across heavier scenes/platforms
- **Frame Time Target**: < 16.67ms per frame (including rendering)
- **CPU Usage**: Reasonable CPU usage (not 100% on one core)
- **Memory Usage**: Efficient memory usage

---

## Quick Start

### Prerequisites

- **Go 1.22 or later** ([Download Go](https://golang.org/dl/))
- **SDL2 Development Libraries**
  - **Ubuntu/Debian**: `sudo apt-get install libsdl2-dev`
  - **Fedora/RHEL**: `sudo dnf install SDL2-devel`
  - **macOS**: `brew install sdl2`
  - **Windows**: Download from [SDL2 website](https://www.libsdl.org/download-2.0.php)

**Optional - SDL2_ttf** (for system fonts):
  - **Ubuntu/Debian**: `sudo apt-get install libsdl2-ttf-dev`
  - **macOS**: `brew install sdl2_ttf`
  - **Windows**: Download from [SDL2_ttf website](https://www.libsdl.org/projects/SDL_ttf/)
  
  *Note: The emulator works fine without SDL2_ttf—it has a built-in bitmap font.*

### Option A: Download a Release (Recommended)

Download the latest prebuilt package for your platform:

- Releases: https://github.com/RetroCodeRamen/Nitro-Core-DX/releases
- Latest release: https://github.com/RetroCodeRamen/Nitro-Core-DX/releases/latest

Package names:
- **Linux**: `nitrocoredx-<version>-linux-amd64.tar.gz`
- **Windows**: `nitrocoredx-<version>-windows-amd64.zip`

After extracting:
- **Linux**: run `./nitrocoredx`
- **Windows**: run `nitrocoredx.exe`

Use **Emulator Focus** view inside the app if you just want to load and play/test ROMs, or **Code Only** view for editing without the emulator visible.

### Option B: Build from Source (Developer Workflow)

1. **Clone the repository:**
   ```bash
   git clone https://github.com/RetroCodeRamen/Nitro-Core-DX.git
   cd Nitro-Core-DX
   ```

2. **Run Nitro-Core-DX (recommended integrated app):**

   ```bash
   go run ./cmd/corelx_devkit
   ```

3. **Optional: build the standalone emulator UI:**
   
   **Without SDL2_ttf (recommended if SDL2_ttf is not installed):**
   ```bash
   go build -tags "no_sdl_ttf" -o nitro-core-dx ./cmd/emulator
   ```
   
   **With SDL2_ttf (if you have SDL2_ttf installed):**
   ```bash
   go build -o nitro-core-dx ./cmd/emulator
   ```

4. **Build a test ROM (optional):**
   ```bash
   go build -o testrom ./cmd/testrom
   ./testrom test.rom
   ```

5. **Optional: run the standalone emulator UI directly:**
   ```bash
   ./nitro-core-dx -rom test.rom
   ```

### Nitro-Core-DX App Quick Start (Recommended)

```bash
go run ./cmd/corelx_devkit
```

- Use **New** to create a project from a template (Blank Game, Minimal Loop, Sprite Demo, Tilemap Demo, Shmup Starter, Matrix Mode Demo)
- Use **Open** to launch the project-centric open dialog (source, ROM, or recent projects), or **Load ROM** to run a prebuilt `.rom` directly
- Click **Build + Run** to compile and run in the embedded emulator
- Switch views: **Split View** (editor + emulator), **Emulator Focus** (emulator only), **Code Only** (editor only)
- Use your OS title-bar controls (double-click title bar or window menu) to maximize/restore
- Use **Tools > UI Density** to switch between Compact and Standard spacing
- Use **Capture Game Input** when you want keyboard input routed to the embedded emulator
- Open **Sprite Lab** and **Tilemap Lab** tabs to create/update assets; use **Apply To Manifest** (recommended) for compiler-ingested asset upserts, or **Apply To Project** for in-source asset blocks
- Watch the top bar **Build State** indicator to confirm whether edits are draft vs validated
- Example CoreLX validation file: `test/roms/devkit_moving_box_test.corelx`

Project asset manifest support:
- The compiler service auto-loads `corelx.assets.json` from the same directory as your `.corelx` file (when present).
- Manifest assets are merged with in-source `asset ...` declarations during build.
- This keeps editor proposals and compiler-produced manifests aligned under one compiler-owned build output.

Known-good ROMs for embedded emulator testing (after generating them locally):
- `roms/input_visual_diagnostic.rom`
- `roms/fm_opmlite_showcase.rom`
- `roms/apu_fm_showcase.rom`
- `roms/nitro_pack_in_demo.rom` via `go run -tags testrom_tools ./Games/NitroPackInDemo`

ROM layout:
- Active runnable ROM artifacts are centralized in `roms/`.
- `test/roms/` contains generators, sample CoreLX sources, and build docs.
- `test/roms/archive/legacy_roms/` contains historical legacy ROM snapshots.

See `test/roms/README_TEST_ROMS.md` for generator commands (`testrom_tools` build tag).

### Release Downloads (GitHub Releases)

Users can download the prebuilt archive for their platform from the **Releases** page:

- Releases: https://github.com/RetroCodeRamen/Nitro-Core-DX/releases
- Latest release: https://github.com/RetroCodeRamen/Nitro-Core-DX/releases/latest

- Linux: `nitrocoredx-<version>-linux-amd64.tar.gz`
- Windows: `nitrocoredx-<version>-windows-amd64.zip`

See `docs/guides/RELEASE_BINARIES.md` for the release build workflow and packaging details.

### Standalone Emulator Command Line Options (Optional)

- `-rom <path>`: Path to ROM file (required)
- `-unlimited`: Run at unlimited speed (no frame limit)
- `-scale <1-6>`: Display scale multiplier (default: 3)
- `-log`: Enable logging (disabled by default)
- `-cyclelog <file>`: Enable cycle-by-cycle logging to a file
- `-maxcycles <N>`: Maximum cycles to log (default `100000`, `0` = unlimited)
- `-cyclestart <N>`: Start cycle logging after cycle `N`

### Standalone Emulator Example Usage

```bash
# Run with default 3x scale
./nitro-core-dx -rom test.rom

# Run at unlimited speed with 4x scale
./nitro-core-dx -rom test.rom -unlimited -scale 4

# Run with 1x scale (native resolution)
./nitro-core-dx -rom test.rom -scale 1

# Run with logging enabled
./nitro-core-dx -rom test.rom -log
```

### Emulator Input Mapping (Integrated App and Standalone Emulator)

- **Arrow Keys / WASD**: D-pad (move/control)
- **Z / X / V / C**: A / B / X / Y buttons
- **Q / E**: L / R buttons
- **Enter**: Start
- **Backspace**: Extra diagnostic/test button (used by some test ROMs)

Note: Test ROMs can map controls differently. Use the ROM-specific docs/comments for expected behavior.

### Troubleshooting

**SDL2 Not Found:**
1. Install SDL2 development libraries (see Prerequisites above)
2. Make sure `pkg-config` can find SDL2: `pkg-config --modversion sdl2`
3. If using a custom SDL2 installation, set `PKG_CONFIG_PATH` environment variable

**Build Errors:**
- Make sure Go is properly installed: `go version` (should show 1.22 or later)
- Make sure all dependencies are downloaded: `go mod download`
- If SDL2_ttf is not installed, use `-tags no_sdl_ttf` for emulator/UI builds and tests
- Clean and rebuild (no SDL2_ttf): `go clean -cache && go build -tags no_sdl_ttf ./...`
- Fast regression suite: `make test-fast` (recommended before longer test runs)

**ROM Generator Utilities:**
- `cmd/testrom` has one main (default); extra tools live in `cmd/testrom/input`, `cmd/testrom/minimal`, `cmd/testrom/cpu-execution`, `cmd/testrom/verify-bytecode` (build with `go build ./cmd/testrom/input`, etc.).
- Generators under `test/roms` (e.g. `build_*.go`) each have their own `main()`; run as single-file utilities with `go run -tags testrom_tools ./test/roms/<file>.go <args>`. Do not run `go test -tags testrom_tools ./test/roms` (multiple mains in one package).

**Runtime Errors:**
- Check that the ROM file exists and is readable
- Verify the ROM file is a valid Nitro-Core-DX ROM (magic number "RMCF")
- Check console output for specific error messages

**Nitro-Core-DX App Input Seems Ignored:**
- Click the embedded emulator pane to give it keyboard focus
- Make sure `Capture Game Input` is enabled in the Dev Kit when testing controls

For more detailed troubleshooting, see [docs/guides/](docs/guides/) (debugging and input guides) and the incident history in [docs/archive/](docs/archive/).

---

## Documentation

The project documentation is organized into several main documents:

### Core Documentation (Start Here)
- **[docs/README.md](docs/README.md)**: Documentation map (current vs historical docs)
- **[SYSTEM_MANUAL.md](SYSTEM_MANUAL.md)**: System architecture manual (under revision; verify against current specs)
- **[PROGRAMMING_MANUAL.md](PROGRAMMING_MANUAL.md)**: Programming manual (under revision; pre-alpha APIs may change)
- **[docs/CORELX.md](docs/CORELX.md)**: Current CoreLX language reference (implementation-aware)
- **[docs/specifications/CORELX_SYNTAX_V1.md](docs/specifications/CORELX_SYNTAX_V1.md)**: CoreLX v1 language syntax charter
- **[docs/specifications/CORELX_CARTRIDGE_FORMAT.md](docs/specifications/CORELX_CARTRIDGE_FORMAT.md)**: CoreLX single-file cartridge format (draft)
- **[docs/specifications/COMPLETE_HARDWARE_SPECIFICATION_V2.1.md](docs/specifications/COMPLETE_HARDWARE_SPECIFICATION_V2.1.md)**: Current evidence-based hardware specification
- **[docs/specifications/APU_FM_OPM_EXTENSION_SPEC.md](docs/specifications/APU_FM_OPM_EXTENSION_SPEC.md)**: FM extension design + implementation status

### Additional Documentation
- **[CHANGELOG.md](CHANGELOG.md)**: Version history and change log
- **[docs/DEVELOPMENT_NOTES.md](docs/DEVELOPMENT_NOTES.md)**: Development process, challenges, and philosophy
- **[docs/DEVKIT_ARCHITECTURE.md](docs/DEVKIT_ARCHITECTURE.md)**: Dev Kit backend/frontend split and invariants
- **[docs/testing/](docs/testing/)**: Testing guides and results
- **[docs/specifications/](docs/specifications/)**: Hardware specs, pin definitions, FPGA docs (with current-vs-historical notes)
- **[docs/guides/](docs/guides/)**: Setup guides, build instructions, and procedures
- **[docs/planning/](docs/planning/)**: Development plans and roadmaps

---

## Features

### Core Emulation

- **Cycle-Counted CPU Emulation**
  - Custom 16-bit CPU with banked 24-bit addressing
  - 8 general-purpose registers (R0-R7)
  - Complete instruction set with precise cycle counting

- **Feature-Complete PPU Rendering (under continued timing/perf validation)**
  - 4 independent background layers (BG0-BG3)
  - 128 sprites with priorities and blending modes
  - Matrix Mode (Mode 7-style effects on multiple layers)
  - Windowing system with proper logic
  - HDMA for per-scanline effects

- **Legacy APU + FM Extension Path**
  - 4 legacy audio channels with waveforms (sine, square, saw, noise)
  - 44,100 Hz sample rate
  - PCM playback support
  - Master volume control
  - FM extension MMIO + timer/IRQ path with YM2608/OPNA playback through the YMFM-backed runtime

- **Precise Memory Mapping**
  - Banked memory architecture (256 banks × 64KB = 16MB address space; ROM uses 32KB LoROM windows in banks 1-125)
  - WRAM (32KB), Extended WRAM (128KB), ROM (up to 7.8MB)
  - I/O register routing

- **ROM Loading and Execution**
  - Proper header parsing (32-byte header)
  - Entry point handling
  - LoROM-style memory mapping

### Tooling

- **Nitro-Core-DX App (Dev Kit)**: Professional IDE with menu bar, domain-grouped toolbar, CoreLX editor, Build/Build+Run, diagnostics panel, embedded emulator, Sprite Lab, Tilemap Lab, project templates, autosave, settings persistence, UI density modes, and three view modes (Split View, Emulator Focus, Code Only)
- **Sprite Lab**: Pixel-art sprite editor with 8x8 to 64x64 canvas, 16 palette banks (16 colors each), RGB555 editing (hex + slider), grid overlay, mirror painting, wrap-shift controls (up/down/left/right), transparent-index checker display, undo/redo history, `.clxsprite` import/export, and Insert/Apply asset flows
- **Tilemap Lab**: Tilemap editor with brush/fill/erase tools, palette/flip attributes, source tileset parsing, tile atlas selection, `.clxtilemap` import/export, and Insert/Apply asset flows
- **CoreLX Compiler**: structured diagnostics, manifest JSON, compile bundle JSON, and project asset manifest ingestion (`corelx.assets.json`) in the compile service path
- **Assembler v1 (`cmd/asm`)**: text assembly to ROM for low-level workflows
- **Logging & Debug Support**: component logging, cycle logger, register/memory viewers, debugger CLI components
- **ROM Test Tooling**: Go-based ROM builders and test generators (some gated by `testrom_tools`)

For detailed information about debugging tools, see [SYSTEM_MANUAL.md](SYSTEM_MANUAL.md) and [docs/README.md](docs/README.md) for current doc routing.

---

## Project Structure

```
nitro-core-dx/
├── cmd/
│   ├── emulator/          # Standalone emulator application
│   ├── corelx/            # CoreLX compiler CLI
│   ├── corelx_devkit/     # Integrated Dev Kit IDE
│   │   ├── main.go            # App entry, UI init, toolbar, view modes
│   │   ├── corelx_code_editor.go # Inline syntax-highlighted CoreLX editor widget
│   │   ├── sprite_lab.go      # Sprite Lab pixel editor
│   │   ├── tilemap_lab.go     # Tilemap Lab map editor + tile atlas picker
│   │   ├── templates.go       # Project templates and New Project dialog
│   │   ├── settings.go        # Settings persistence (JSON)
│   │   ├── autosave.go        # Autosave and crash recovery
│   │   ├── help_center.go     # Menu bar structure (File/Edit/View/Build/Debug/Tools/Help)
│   │   └── compact_theme.go   # UI density theme (Compact/Standard)
│   ├── asm/               # v1 text assembler CLI
│   ├── testrom/           # Default test ROM generator
│   │   ├── input/         # Input test ROM generator
│   │   ├── minimal/       # Minimal sprite test ROM generator
│   │   ├── cpu-execution/ # CPU execution simulator (ROM analysis)
│   │   └── verify-bytecode/ # ROM bytecode/JMP/branch verifier
│   └── ...
├── internal/
│   ├── cpu/               # CPU emulation
│   ├── memory/            # Memory system
│   ├── ppu/               # Graphics system
│   ├── apu/               # Audio system
│   ├── input/             # Input system
│   ├── ui/                # User interface
│   ├── emulator/          # Emulator orchestration
│   ├── corelx/            # CoreLX compiler
│   ├── asm/               # Assembler implementation
│   ├── devkit/            # UI-agnostic Dev Kit backend
│   └── debug/             # Debugging tools
├── roms/                  # Active runnable ROM artifacts
├── test/
│   └── roms/              # ROM generators, sample CoreLX programs, and build helpers
├── docs/
│   ├── issues/            # Known issues and fixes
│   ├── testing/           # Testing guides
│   ├── planning/          # V1 charter, acceptance criteria, risks
│   ├── specifications/    # Hardware specifications
│   └── guides/            # Programming guides and tutorials
├── README.md              # This file
├── SYSTEM_MANUAL.md       # System architecture
├── PROGRAMMING_MANUAL.md  # Programming guide
└── CHANGELOG.md           # Version history
```

---

## Contributing

Contributions are welcome! This project is in active development.

**Getting Started:**
1. Read the [README.md](README.md) for project overview
2. Read [docs/README.md](docs/README.md) for the current documentation map
3. Read the [CoreLX Documentation](docs/CORELX.md) for current language behavior
4. Use [PROGRAMMING_MANUAL.md](PROGRAMMING_MANUAL.md) and [SYSTEM_MANUAL.md](SYSTEM_MANUAL.md) as manuals under revision, verifying details against current specs/tests

**Development Status:**
✅ **Architecture Stable**: Core hardware architecture is stable; active work continues on tooling, tests, and documentation alignment.

**Code Style:**
- Follow Go conventions and best practices
- Use `go fmt` to format code
- Write clear, commented code
- Add tests where appropriate

**Pull Request Process:**
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test thoroughly
5. Submit a pull request with a clear description

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## Acknowledgments

- **SNES**: For showing what beautiful 16-bit graphics could look like
- **Sega Genesis**: For proving that speed matters just as much as looks
- **The Retro Gaming Community**: For keeping the spirit of 16-bit gaming alive
