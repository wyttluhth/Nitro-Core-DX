# Changelog

All notable changes to the Nitro-Core-DX project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

**Note:** This changelog was created on January 27, 2026. Previous changes have been reconstructed from project documentation and commit history.

---

## [Unreleased]

---

## [0.2.0] - 2026-06-12

### Added
- **NitroPackInDemo ROM-first pack-in demo workspace**
  - Added `Games/NitroPackInDemo/` as the canonical home for the ROM-first pack-in/sample demo effort.
  - Includes the locked design doc, ROM builder, scene-flow tests, and placeholder `park.png` / `building.png` assets for the current vertical slice.
- **ROM builder branch helper coverage**
  - Added `BGT`, `BLT`, `BGE`, and `BLE` convenience branch helpers to `test/roms/romutil/asm.go` so current ROM-side control-flow builders can target signed comparisons cleanly.
- **CoreLX v1 language design (M7 design extraction complete)**
  - Added `Games/NitroPackInDemo/CORELX_EXTRACTION.md`: the full M7 system-by-system extraction of the demo ROM into proposed CoreLX APIs, plus the settled v1 design decision record (memory model, modules, generic transformation planes, three-tier builtin/module/pattern test, stability contract).
  - Added `docs/specifications/CORELX_SYNTAX_V1.md`: the CoreLX v1 syntax charter (learnability-first; Lua/BASIC/Go anchors; `:=`/`var`/`const`, `int`+`fixed` with decimal literals as fixed-point, structs as reference types, BASIC `for ... to` loops, data declarations as the section grammar).
  - Added `docs/specifications/CORELX_CARTRIDGE_FORMAT.md`: single-file cartridge format draft (one text file = whole game; sections for sprites/backgrounds/audio; import-time binary-to-text conversion; lossless editor round-trip contract).

### Changed
- **NitroPackInDemo complete demo loop (M4-M7)**
  - The interior placeholder scene is now a full matrix-plane room: a procedural checkered floor on plane 2 (BG2) and a procedural NPC guide billboard on plane 3 (BG3), with room-bounds clamping, NPC collision, and the same feet-pivot camera model as the overworld.
  - Added a two-page typewriter dialogue scene (one character revealed per frame, A skips then advances) streamed from WRAM character tables, triggered by talking to the guide.
  - Added a credits scene reached from the final dialogue page; START on the credits performs a full state reset back to the title so the demo loops cleanly.
  - The building door now initializes the interior entry state, and the interior exit zone returns to the overworld with the outdoor position preserved.
  - Extended `build_rom_test.go` to drive the full title → overworld → interior → dialogue → credits → title loop headlessly, including NPC collision and the restarted second lap.
- **NitroPackInDemo overworld baseline**
  - The current demo ROM now has a title scene, overworld floor + facade slice, pause overlay, centered placeholder player sprite, and enterable interior placeholder scene under `Games/NitroPackInDemo/`.
  - The building facade is now driven from the same camera/horizon/focal model as the overworld floor so the scene can be tuned as one coherent projection.
- **Documentation alignment and deep cleanup**
  - Updated active manuals and README to reflect the current vertical-projected-quad semantics and the ROM-first `NitroPackInDemo` effort.
  - Comprehensive documentation pass: deleted 15 dead docs (resolved-issue postmortems, stale meta-cleanup notes), archived 10 historical plans/audits to `docs/archive/`, rewrote the `docs/README.md` navigation map around the CoreLX v1 sources of truth, fixed the stale "user-defined functions: Planned" status in `docs/CORELX.md`, and added scope notes distinguishing current-compiler docs from the v1 charter.
  - Disambiguated the two "V1"s: the product V1 charter (`docs/planning/V1_CHARTER.md`) and the CoreLX language v1 charter (`docs/specifications/CORELX_SYNTAX_V1.md`) now cross-reference each other.
- **Makefile `test-full` scoped to project packages**
  - `test-full` no longer sweeps vendored reference code under `Resources/` (which requires C libraries the project does not depend on), and now also runs the `testrom_tools`-gated NitroPackInDemo ROM-builder tests as a second pass.

### Removed
- **GalaxyForce prototype workspace**
  - Removed `Games/GalaxyForce/` for now so the active repo stays focused on the ROM-first `NitroPackInDemo` proof-of-concept and current emulator/dev-kit priorities.
- **Repository clutter**
  - Removed an accidentally committed third-party installer (`autodesk_fusion_installer_x86-64.sh`) and local build artifacts/logs from the working tree (all rebuildable; already gitignored).

### Fixed
- **Vertical projected quad ground anchoring / projection correctness**
  - Replaced the old projected-corner interpolation path in `internal/ppu/scanline.go` with a world-space ray/plane intersection path for vertical projected quads.
  - Height sampling now uses camera-space forward depth, which keeps facades better tied to the ground plane and reduces the old screen-facing Doom-sprite behavior.
- **Projection regression coverage**
  - Added dedicated PPU tests covering approach behavior and center-anchor agreement for vertical projected quads in `internal/ppu/features_test.go`.
- **NitroPackInDemo scene and projection checks**
  - Added ROM-side assertions that the demo building plane inherits the overworld floor camera model in `Games/NitroPackInDemo/build_rom_test.go`.
- **SpriteProbe ship test**
  - `TestShip_Visible` now skips cleanly when its local-only `ship.corelx` source is absent (lost in a machine migration) instead of failing the suite.

---

## [0.1.9] - 2026-03-11

### Added
- **Dedicated Matrix Plane Architecture (Emulator Baseline)**
  - Added dedicated matrix-plane tilemap memory, dedicated matrix-plane pattern memory, bitmap-backed matrix planes, clamp outside mode, and matrix-plane upload MMIO/programming paths.
  - Added matrix-plane builder/service APIs and CoreLX matrix-plane helpers (`enable`, `load_tiles`, `load_tilemap`, `set_tile`, `fill_rect`, `clear`).
  - Added matrix-plane showcase ROMs and bitmap import/capture tooling for matrix validation from real images.
  - Locations: `internal/ppu/`, `internal/emulator/matrix_plane*.go`, `internal/corelx/*`, `test/roms/build_matrix_*`, `cmd/matrixpng_capture/`
- **Release Package Test ROMs**
  - Added bundled release-package ROMs for runtime validation:
    - `roms/pong_ym2608_demo.rom`
    - `roms/matrix_floor_only_kart.rom`
  - Updated local and GitHub release packaging to include these ROMs in downloadable archives.
  - Locations: `scripts/package_release.sh`, `.github/workflows/release-binaries.yml`

### Changed
- **ROM Path and Test Generator Cleanup**
  - Centralized active runnable ROM artifacts under the root `roms/` directory.
  - Updated active docs/scripts to use `roms/...` outputs for consistent run/build/test commands.
  - Retired legacy duplicate command path `cmd/testrom_input` in favor of `cmd/testrom/input`.
  - Why this changed: reduce tooling drift, eliminate duplicate command maintenance, and make Dev Kit/default ROM discovery predictable across workflows.
- **YM2608 Runtime Path Simplification**
  - Removed legacy FM fallback behavior from the emulator/devkit runtime path and aligned builds around the YMFM-backed YM2608 runtime.
  - Why this changed: ensure release testing happens against the intended audio path instead of a silent fallback.
- **Compact Song Storage For YM2608 Demo ROMs**
  - Replaced code-generated per-frame YM write playback with compact banked song data + ROM playback driver for the active Pong demo path.
  - Why this changed: allow full-song playback and realistic multi-song cartridge budgeting.
- **CPU Contract Cleanup**
  - Resolved the old `CMP immediate` / `BEQ` encoding ambiguity and aligned assembler/runtime behavior around the cleaned-up encoding.
  - Why this changed: remove heuristic ISA behavior and make the active CPU contract easier to target and document.

---

## [0.1.8] - 2026-03-06

### Added
- **Native Editor Engine Foundation** (single-ownership model)
  - Replaced fragile hidden input forwarding pattern with a native document model path for editor interaction handling
  - Added internal editor model package with gap-buffer-based text storage and caret/selection primitives
  - Added editor interaction tests for typing and selection forwarding behavior
  - Location: `internal/editor/native/`, `cmd/corelx_devkit/corelx_code_editor.go`, `cmd/corelx_devkit/*_test.go`
- **Sprite Lab Wrap-Shift Controls**
  - Added sprite-wide wrapped shift operations (up/down/left/right)
  - Added hotkeys for wrapped shifting (`I/J/K/L`) in Sprite Lab
  - Location: `cmd/corelx_devkit/sprite_lab.go`
- **Palette Value Slider in Sprite Lab**
  - Added RGB555 slider-based color editing with synchronized hex value display
  - Kept full-width hex entry visible during palette editing flows
  - Location: `cmd/corelx_devkit/sprite_lab.go`
- **Window Behavior Guardrail Test**
  - Added test coverage to ensure platform maximize-hint calls remain constrained and do not sprawl into unrelated UI paths
  - Location: `cmd/corelx_devkit/window_behavior_test.go`
- **Weekly Blog Log**
  - Added running blog post document with 2026-03-06 entry covering weekly progress and planning updates
  - Location: `docs/BLOG_POSTS.md`

### Changed
- **Sprite Lab Preview Scaling**
  - Switched preview rendering to aspect-preserving containment to prevent distortion on window resize
  - Location: `cmd/corelx_devkit/sprite_lab.go`
- **Window Management Policy**
  - Reaffirmed native OS maximize/minimize behavior as a release-quality invariant and documented it in planning/architecture docs
  - Startup applies X11 maximize compatibility hint in a constrained path where required
  - Location: `cmd/corelx_devkit/main.go`, `cmd/corelx_devkit/window_x11_maximize.go`, planning docs
- **V1 Audio Direction**
  - Updated V1 planning scope from YM2151/OPM-lite release target to YM2608 release target
  - Added explicit execution-order constraints: Sprite/Dev Kit -> Tilemap flow -> Sound Studio -> YM2608
  - Location: `docs/planning/V1_CHARTER.md`, `docs/planning/V1_ACCEPTANCE.md`, `docs/planning/V1_RISKS.md`
- **Documentation Alignment Pass**
  - Updated manual/guide/spec references to reflect current editor direction, Sprite Lab capabilities, and YM2608 target planning state
  - Location: `README.md`, `PROGRAMMING_MANUAL.md`, `docs/CORELX.md`, `docs/guides/PROGRAMMING_GUIDE.md`, `docs/specifications/README.md`, `docs/specifications/APU_FM_OPM_EXTENSION_SPEC.md`

### Fixed
- **Dev Kit Editor Interaction Stability**
  - Improved reliability for typing/selection paths that previously regressed under rapid editor feature iteration
- **Linux Maximize Availability Regression**
  - Restored expected title-bar maximize behavior in environments that required explicit hinting

---

## [0.1.7] - 2026-02-28

### Added
- **Sprite Lab** — Built-in pixel-art sprite editor integrated as a workbench tab
  - Canvas sizes from 8x8 to 64x64 (step of 8) with dynamic cell sizing for uniform pixels
  - 16 palette banks with 16 colors each (RGB555 format)
  - Pencil and Erase tools with Mirror X painting mode
  - Grid overlay with scaled line thickness and hover highlighting
  - Undo/Redo history (up to 128 states)
  - Import/Export `.clxsprite` asset files (JSON format with palette bank data)
  - One-click **Insert CoreLX Asset** generates tile hex data + palette setup code directly into the editor
  - Preview pane with packed 4bpp hex output and copy-to-clipboard
  - Location: `cmd/corelx_devkit/sprite_lab.go`
- **Project Templates** — Six built-in templates for new project creation
  - Blank Game, Minimal Loop, Sprite Demo, Tilemap Demo, Shmup Starter, Matrix Mode Demo
  - Accessible via **New** button in toolbar or **File > New Project** menu
  - Location: `cmd/corelx_devkit/templates.go`
- **IDE Menu Bar** — Traditional IDE menu structure (File, Edit, View, Build, Debug, Tools, Help)
  - Replaces ad-hoc menu; establishes professional SDK identity
  - Location: `cmd/corelx_devkit/help_center.go`
- **Three View Modes** — Split View, Emulator Focus, Code Only
  - Code Only mode hides the emulator entirely for focused editing
  - Dynamic layout construction prevents Fyne widget re-parenting issues
- **UI Density Modes** — Compact and Standard spacing via Tools menu
  - Custom Fyne theme with reduced padding, font sizes, and line spacing for Compact mode
  - Persisted in settings between sessions
  - Location: `cmd/corelx_devkit/compact_theme.go`
- **Autosave** — Automatic crash recovery for unsaved editor work
  - Location: `cmd/corelx_devkit/autosave.go`
- **Settings Persistence** — View mode, split offsets, recent files, UI density, and last directories saved between sessions
  - Location: `cmd/corelx_devkit/settings.go`
- **Load ROM Toolbar Button** — Direct ROM loading for testing without recompilation, added to both toolbar and File menu
- **Window Maximize (F11)** — F11 toggles programmatic maximize/restore with X11 WM hint support on Linux
  - Location: `cmd/corelx_devkit/window_x11_maximize.go`

### Changed
- **IDE Toolbar Redesign** — Domain-grouped toolbar (Project, Build, Run/Debug, View) with visual separators replacing flat button row
- **Terminology Update** — Project-centric language throughout (replaced ROM-centric phrasing); "Hardware Output" instead of "Emulator" in status labels
- **UI Compactness Overhaul** — Shortened toolbar labels, compacted panel headers, removed redundant controls, tightened spacing across all panels
- **Emulator No Longer Auto-Runs** — Emulator does not start running on project load; requires explicit Build + Run
- **Fullscreen Removed** — Replaced with maximize/restore toggle (F11) for more reliable window management on Linux
- **Documentation Update** — README, Programming Manual, Programming Guide, Hardware Features Status, V1 Charter, and V1 Acceptance all updated to reflect new IDE features and Sprite Lab

### Fixed
- **Sprite Lab Pixel Uniformity** — Dynamic cell-size computation ensures every sprite pixel maps to exactly the same number of screen pixels
- **Sprite Lab Paint Performance** — Only editor canvas re-renders during paint strokes; preview, hex dump, and palette updates deferred to stroke end
- **Sprite Lab Loading Lag** — Eliminated redundant initial image renders; placeholder images used until first `refreshVisuals` call
- **Sprite Lab Mouse Alignment** — Proportional coordinate mapping with `ImageFillStretch` and `NewCenter` constraint ensures correct cell targeting
- **Sprite Lab Grid Consistency** — Grid line thickness scales with cell size (2px for cells ≥16px, 1px for cells ≥4px) to prevent inconsistent appearance under display scaling
- **Split View Rendering** — Fixed Fyne widget re-parenting issue by dynamically building all layout containers in `setViewMode` instead of pre-building them
- **Window Maximize on Linux** — X11 `WM_NORMAL_HINTS` set programmatically to enable native WM maximize after Fyne/GLFW initialization

---

## [Historical Notes (Pre-0.1.7 Reconstruction)]

### Added
- **Integrated Dev Kit (CoreLX + Embedded Emulator) MVP** (2026-02-24)
  - Added `cmd/corelx_devkit` with CoreLX editor, diagnostics/output panels, manifest view, embedded emulator, `Build`, `Build + Run`, and `Load ROM`
  - Added view modes (`Full View`, `Emulator Only`) and embedded emulator audio/input handling in the Dev Kit
  - Location: `cmd/corelx_devkit/`, `internal/devkit/`
- **Dev Kit Backend Service Layer** (2026-02-24)
  - Added UI-agnostic Dev Kit backend wrapper for compiler + emulator session management, frame ticking, framebuffer/audio snapshots, and ROM loading
  - Enables frontend replacement without changing emulator/core behavior
  - Location: `internal/devkit/`
- **CoreLX Compiler Production API + Structured Diagnostics (Phase 1)** (2026-02-24)
  - Added `CompileProject/CompileSource/CompileFile`, compiler service wrapper, structured diagnostics, manifest/build bundle JSON outputs, and pack-stage budget diagnostics
  - Added normalized asset pipeline foundation and multi-section manifest accounting for tooling integration
  - Location: `internal/corelx/*`, `internal/rom/builder.go`
- **Banked ROM Builder Skeleton** (2026-02-24)
  - Added initial bank-aware ROM builder/linker skeleton with tests for bank-local labels and relative relocations
  - Location: `internal/rom/banked_builder.go`, `internal/rom/banked_builder_test.go`
- **FM/OPM Extension Skeleton and Diagnostics** (2026-02-24)
  - Added APU FM extension MMIO host interface (`0x9100+`), timer/status/IRQ behavior, bus integration tests, and audible OPM-lite software FM path
  - Added FM showcase and APU/FM test ROM generators
  - Location: `internal/apu/fm_opm.go`, `internal/apu/fm_opm_test.go`, `internal/memory/bus_fm_integration_test.go`, `test/roms/build_apu_fm_showcase.go`, `test/roms/build_fm_opmlite_showcase.go`
- **Text Assembler v1 (`.asm` -> `.rom`)** (2026-02-24)
  - Added assembler package and CLI with labels, branches, `.entry`, `.word`, and support for the current CPU ISA
  - Location: `internal/asm/`, `cmd/asm/`
- **CoreLX Dev Kit Test Programs** (2026-02-24)
  - Added CoreLX compile/run validation ROM sources including moving-box test and color/input test
  - Location: `test/roms/devkit_moving_box_test.corelx`, `test/roms/devkit_compile_run_color_test.corelx`
- **Documentation/Architecture Additions** (2026-02-24)
  - Added CoreLX data model plan, Dev Kit architecture doc, FM extension spec, and future feature parking lot
  - Location: `docs/CORELX_DATA_MODEL_PLAN.md`, `docs/DEVKIT_ARCHITECTURE.md`, `docs/specifications/APU_FM_OPM_EXTENSION_SPEC.md`, `docs/planning/FUTURE_FEATURES_PARKING_LOT.md`

### Changed
- **Documentation Cleanup and Source-of-Truth Reorganization** (2026-02-24)
  - Reworked docs indexes, marked historical docs explicitly, and archived redundant testing snapshots into `docs/archive/test_results/`
  - Updated README/spec/hardware status docs to reflect current APU/FM and tooling state
  - Location: `docs/README.md`, `docs/*/README.md`, `docs/archive/test_results/*`, `README.md`, `docs/HARDWARE_FEATURES_STATUS.md`, spec docs
- **Programming Manual Rewrite (Pre-Alpha)** (2026-02-24)
  - Rewrote `PROGRAMMING_MANUAL.md` as a CoreLX-first, beginner-friendly guide with current Dev Kit workflows and a separate assembly section
  - Removed outdated mixed-mode inline assembly claims and aligned with current implementation status
  - Location: `PROGRAMMING_MANUAL.md`
- **Input Visual Diagnostic ROM Expanded** (2026-02-24)
  - Upgraded to broader system validation (movement, palette/background changes, audio interactions, layered music, FM MMIO exercise)
  - Location: `test/roms/build_input_visual_diagnostic.go`, `test/roms/README_TEST_ROMS.md`
- **PPU/Core Performance and UI Pacing Improvements** (2026-02-24)
  - Additional render-path optimizations and Fyne UI pacing/input improvements for smoother visible emulation in Dev Kit/Fyne UI
  - Location: `internal/ppu/scanline.go`, `internal/ui/fyne_ui.go`, `cmd/corelx_devkit/main.go`

### Fixed
- **CoreLX Sprite/Palette/Input Runtime Bugs** (2026-02-24)
  - Fixed CoreLX palette writes to CGRAM address units, OAM sprite write helper register clobbering, local variable register corruption, and broken branch patching/condition codegen
  - Restored correct input-driven sprite movement/color behavior in CoreLX Dev Kit test ROMs
  - Location: `internal/corelx/codegen.go`
- **Dev Kit Embedded Emulator Input Capture/Focus** (2026-02-24)
  - Fixed startup nil dereference in diagnostics filter init
  - Fixed emulator keyboard focus/capture behavior by adding focusable emulator input overlay and explicit capture behavior
  - Location: `cmd/corelx_devkit/main.go`
- **Emulator/APU Runtime Integration Issues** (2026-02-24)
  - Restored frame-based APU update call in emulator loop for note duration/completion behavior
  - Restored embedded UI audio queueing after Fyne UI refactor
  - Location: `internal/emulator/emulator.go`, `internal/ui/fyne_ui.go`
- **APU Audio Quality / FM Test Timing Issues** (2026-02-24)
  - Corrected fixed-point saw scaling/clipping issue and improved FM note transition/timing behavior for showcase ROMs
  - Location: `internal/apu/fixed_point.go`, `internal/apu/fm_opm.go`, `test/roms/build_fm_opmlite_showcase.go`

### Added
- **Local Test Tier Workflow** (2026-02-23)
  - Added root `Makefile` targets for repeatable test runs (`test-fast`, `test-emulator`, `test-commands`, `test-full`, `test-long`)
  - Updated testing docs for local `no_sdl_ttf` workflows and generator build tags
  - Location: `Makefile`, `docs/testing/README.md`, `docs/testing/TEST_SUMMARY.md`
- **Input Visual Diagnostic ROM (v2)** (2026-02-23)
  - Added a robust manual diagnostic ROM generator for input/sprite/background/audio verification
  - Includes button-driven sprite movement, palette/background toggles, reset, and note start/stop controls
  - Location: `test/roms/build_input_visual_diagnostic.go`, `test/roms/README_TEST_ROMS.md`
- **CoreLX Nitro Core 8 Compiler Design Doc** (2026-02-23)
  - Added target-profile-based CoreLX compiler design for Nitro Core 8 with implementation prompt
  - Location: `docs/specifications/CORELX_NITRO_CORE_8_COMPILER_DESIGN.md`

### Changed
- **Status/Testing Documentation Alignment** (2026-02-23)
  - Reconciled README/testing/planning docs with verified local baseline and current test commands
  - Clarified `no_sdl_ttf` usage and historical-plan caveats
  - Location: `README.md`, `docs/archive/plans/MASTER_PLAN_CONSOLIDATED_2026-01.md`, `docs/testing/*`
- **PPU Render Path Performance (Hardware-Safe Refactors)** (2026-02-23)
  - Reduced per-pixel allocations using reusable scratch buffers
  - Added scanline sprite evaluation cache (hardware-style sprite pipeline stage)
  - Added CGRAM RGB888 conversion cache (derived software cache, behavior-preserving)
  - Location: `internal/ppu/ppu.go`, `internal/ppu/scanline.go`

### Fixed
- **PPU DMA Legacy Execution Loop Hang** (2026-02-23)
  - Fixed compatibility `executeDMA()` loop to terminate correctly when DMA completes
  - Location: `internal/ppu/ppu.go`
- **CPU Interrupt Tests False Failures** (2026-02-23)
  - Replaced stateless test memory mock with in-memory backing storage so interrupt vector tests validate real behavior
  - Location: `internal/cpu/cpu_test.go`
- **Interrupt Vector Bus Mapping** (2026-02-23)
  - Fixed bus handling for system vectors at `bank0:0xFFE0-0xFFFF`, preventing invalid VBlank IRQ vector failures in ROMs
  - Location: `internal/memory/bus.go`
- **Fyne UI Input Passthrough** (2026-02-23)
  - Added reliable Fyne key down/up handling and merged Fyne key state into emulator input bitmask
  - Location: `internal/ui/fyne_ui.go`
- **PPU Feature/Test Issues** (2026-02-23)
  - Fixed sprite blending RGB888 behavior and Matrix Mode direct-color rendering
  - Updated/repair PPU feature tests and emulator timing assumptions for current constants
  - Location: `internal/ppu/scanline.go`, `internal/ppu/features_test.go`, `internal/emulator/frame_order_test.go`
- **Test Build Structure for `go test ./...`** (2026-02-23)
  - Added build tags to ROM/test generator helper binaries to avoid multi-`main` package test failures
  - Location: `cmd/testrom/*`, `test/roms/*.go`

### Added
- **Determinism Test Harness** - Added comprehensive determinism testing framework (2026-02-05)
  - Tests that debug mode (cycle-by-cycle) and optimized mode (chunk-based) produce identical results
  - Per-frame state hashing (CPU registers, WRAM, framebuffer) for verification
  - Location: `internal/emulator/determinism_test.go`
- **Audio Timing Tests** - Added long-run audio timing tests (2026-02-05)
  - Tests verify fractional accumulator prevents timing drift over extended runs
  - Tests run for 60 frames and 1000 frames to verify accuracy
  - Location: `internal/emulator/audio_timing_test.go`
- **CPU Instruction Correctness Tests** - Added comprehensive test suite for CPU instruction correctness (2026-02-05)
  - `baseline_correctness_test.go` - Tests CMP immediate, signed branch conditions, interrupt entry/exit, MOV reserved modes
  - `ppu/baseline_correctness_test.go` - PPU correctness tests
  - Location: `internal/cpu/`, `internal/ppu/`

### Fixed
- **CMP Immediate Instruction** - Fixed CMP immediate mode decoding (2026-02-05)
  - Resolved ambiguity between CMP immediate (mode 1 with registers) and BEQ (mode 1 with reg1=reg2=0)
  - CMP immediate now correctly distinguished from BEQ branch instruction
  - Location: `internal/cpu/instructions.go` - `executeCMPAndBranches()`
- **Signed Branch Conditions** - Fixed signed branch instructions to use overflow flag correctly (2026-02-05)
  - BGT: Now uses `!Z && (N == V)` instead of `!Z && !N`
  - BLT: Now uses `N != V` instead of just `N`
  - BGE: Now uses `N == V` instead of `!N`
  - BLE: Now uses `Z || (N != V)` instead of `Z || N`
  - Properly handles signed comparisons with overflow detection
  - Location: `internal/cpu/instructions.go` - `executeCMPAndBranches()`
- **RET Instruction Stack Handling** - Enhanced RET instruction with proper interrupt return support (2026-02-05)
  - RET now detects interrupt returns by checking for flags on stack
  - Properly pops Flags (if present), PCOffset, and PBR in correct order
  - Added extensive debug logging and validation for stack pointer changes
  - Handles both CALL returns (2 values) and interrupt returns (3 values)
  - Location: `internal/cpu/instructions.go` - `executeRET()`
- **Pop16 Stack Pointer Modification** - Fixed Pop16 to properly modify stack pointer (2026-02-05)
  - Added validation to ensure SP changes correctly during pop operations
  - Fixed stack pointer tracking for interrupt return handling
  - Location: `internal/cpu/instructions.go` - `Pop16()`

### Fixed
- **Audio Timing Drift** - Fixed audio timing drift by replacing integer division with fractional accumulator (2026-02-05)
  - Replaced `apuCyclesPerSample := uint64(c.CPUSpeed / c.APUSpeed)` with fixed-point fractional accumulator
  - Uses 32-bit fractional part to track exact cycles per sample (7670000 / 44100 ≈ 173.923 cycles)
  - Prevents timing drift over long runs (verified via 1000-frame test)
  - Location: `internal/clock/scheduler.go` - Added `APUFractionalAccumulator` field

### Changed
- **Scheduler-Driven Execution Model** - Restored scheduler authority in optimized mode (2026-02-05)
  - Optimized mode now uses scheduler-driven chunk-based stepping (1000 cycles per chunk)
  - Both debug and optimized modes use the same scheduler, ensuring CPU, PPU, and APU advance on the same cycle timeline
  - Removed "CPU full frame then PPU full frame" execution pattern that bypassed scheduler
  - Maintains cycle-accurate synchronization while improving performance
  - Verified via determinism test: debug and optimized modes produce identical results
  - Location: `internal/emulator/emulator.go` - `RunFrame()`
- **Documentation Reorganization** - Reorganized and cleaned up documentation structure (2026-01-31)
  - Moved narrative/bloggy sections from README to `docs/DEVELOPMENT_NOTES.md`
  - Organized fix/issue documents into `docs/issues/` directory
  - Organized testing documents into `docs/testing/` directory
  - Organized specification documents into `docs/specifications/` directory
  - Simplified README to be concise and reference-based
  - Created README files in each docs subdirectory for navigation
  - Location: Multiple files reorganized
- **README Improvements** - Restored backstory and vision sections while keeping it clean (2026-01-31)
  - Restored "Meet Nitro-Core-DX" section with project introduction
  - Restored "The Vision: Best of Both Worlds" section explaining SNES/Genesis inspiration
  - Restored "Why Go?" section explaining language choice
  - Restored console design images section
  - Kept troubleshooting/problem-solving narrative in `docs/DEVELOPMENT_NOTES.md`
  - README now has backstory while remaining clean and organized
  - Location: `README.md`

### Added
- **GUI Logging Controls** - Added logging component controls in Debug menu
  - Enable/disable logging for CPU, PPU, APU, Memory, Input, UI, System components
  - "Enable All Logging" and "Disable All Logging" options
  - Location: `internal/ui/fyne_ui.go` - Debug menu
- **Input Debug Logging** - Added debug logging for input system
  - Logs all input reads and writes with offset and value
  - Helps diagnose input issues and verify latch behavior
  - Location: `internal/memory/bus.go` - input I/O logging
- **Input System Unit Tests** - Created comprehensive unit tests for input system
  - Tests latch behavior, edge-triggered latching, multiple buttons, controller 2
  - Location: `internal/input/input_test.go`
- **Test ROM Input Generator** - Created test ROM generator for input testing
  - Generates ROM that displays sprite moved by arrow keys/WASD
  - Tests input latching, button reading, and sprite movement
  - Location: `cmd/testrom_input/main.go`
- **Input Testing Guide** - Created testing documentation
  - Manual and automated testing procedures
  - Expected behavior and controls
  - Location: `INPUT_TESTING_GUIDE.md`

### Changed
- **CoreLX Debugging Documentation** - Created `CORELX_DEBUGGING_ISSUES.md` to track compiler bugs and debugging progress
  - Documents fixed compiler bugs (VRAM address calculation, binary operations)
  - Tracks ongoing blank screen issue with CoreLX-compiled ROMs
  - Provides test ROMs and debugging checklist
  - Location: `CORELX_DEBUGGING_ISSUES.md`
- **Fyne Log Viewer Panel** - Implemented log viewer panel for Fyne UI
  - Text selection and copy functionality (Ctrl+C)
  - Component and log level filtering
  - Auto-scroll and save logs functionality
  - Location: `internal/ui/panels/log_viewer_fyne.go`
- **CoreLX Test ROMs** - Created test ROMs for debugging compiler issues
  - `moving_sprite_colored.corelx` - Recreation of working Go ROM in CoreLX
  - `moving_sprite_colored_simple.corelx` - Simplified version with hardcoded values
  - Location: `test/roms/`

### Changed
- **Input System Refactor** - Refactored input system to match FPGA latch-based behavior (2026-01-31)
  - Changed from direct button state reading to latch-based serial shift register interface
  - Implements edge-triggered latching (captures on 0->1 transition)
  - Latched state persists until next latch, matching FPGA behavior
  - Location: `internal/input/input.go`
- **Test ROM Wrapping Logic** - Improved sprite position wrapping in test ROM (2026-01-31)
  - Uses BGT (Branch if Greater Than) for proper X > 319 check
  - Handles unsigned wrap (65535) and signed comparison (X >= 320)
  - Location: `cmd/testrom_input/main.go`

### Fixed
- **Input System FPGA Compatibility** - Fixed input system to match FPGA specification (2026-01-31)
  - Input now uses latch mechanism: write 1 to latch register captures button state
  - Reading input returns latched state, not current state
  - Edge-triggered latching ensures proper button capture timing
  - Location: `internal/input/input.go` - `Write8` and `Read8` methods
- **Savestate Input Fields** - Fixed savestate to use new input system field names (2026-01-31)
  - Updated from `LatchActive`/`Controller2LatchActive` to `Controller1Latched`/`Controller2Latched`
  - Added `Controller1LatchState` and `Controller2LatchState` fields
  - Location: `internal/emulator/savestate.go`
- **Memory Bus Logger Support** - Added logger support to memory bus for input debugging (2026-01-31)
  - Bus now has logger field and SetLogger method
  - Enables input debug logging through bus
  - Location: `internal/memory/bus.go`
- **CoreLX Compiler: VRAM Address Calculation** - Fixed `tiles16` VRAM address calculation
  - Changed from `base * 32` to `base * 128` for 16x16 tiles
  - Impact: 16x16 tiles now load to correct VRAM addresses
  - Location: `internal/corelx/codegen.go` - `generateInlineTileLoad()`
- **CoreLX Compiler: Binary OR Operation** - Fixed register usage in binary OR expressions
  - Left result was saved to R1 but operation used destReg (R0)
  - Now correctly uses R1 for OR operation then moves result to destReg
  - Impact: Expressions like `SPR_PAL(1) | SPR_PRI(0)` now work correctly
  - Location: `internal/corelx/codegen.go` - `BinaryExpr` case `TOKEN_PIPE`
- **CoreLX Compiler: Binary AND Operation** - Fixed register usage in binary AND expressions
  - Same fix as OR operation
  - Impact: Bitwise AND operations now work correctly
  - Location: `internal/corelx/codegen.go` - `BinaryExpr` case `TOKEN_AMPERSAND`
- **CoreLX Compiler: Binary ADD/SUB Operations** - Fixed to restore left result before operation
  - Left result saved to R1, but operations used destReg directly
  - Now restores left result from R1 to destReg before performing operation
  - Impact: Addition and subtraction expressions now work correctly
  - Location: `internal/corelx/codegen.go` - `BinaryExpr` cases `TOKEN_PLUS` and `TOKEN_MINUS`
- **PPU Logging Performance** - Optimized PPU logging to reduce performance impact
  - OAM logging limited to first 4 sprites, every 60 frames
  - VRAM logging limited to first 32 bytes, first frame only
  - CGRAM logging limited to first 20 colors, first frame only
  - Impact: Reduced logging overhead from 30 FPS to 7 FPS back to ~30 FPS
  - Location: `internal/ppu/ppu.go`

### Changed
- **UI Consolidation** - Removed all SDL2-based UI code, using Fyne exclusively
  - Deleted: `internal/ui/ui.go`, `ui_render.go`, `menu.go`, `toolbar.go`, `statusbar.go`, `font.go`
  - Deleted: `internal/ui/panels/log_viewer.go`, `log_controls.go`
  - Removed redundant toolbar (controls now only in menu)
  - Location: `internal/ui/`
- **Fyne UI Layout** - Improved resizable log viewer and dynamic panel visibility
  - Log viewer and debug panels now hide when disabled
  - Splitter adjusts automatically based on panel visibility
  - Location: `internal/ui/fyne_ui.go`
- **CoreLX Compiler Entry Point** - Enhanced `__Boot()` function support
  - Compiler now ensures `__Boot()` is generated first if present
  - Sets entry point to 0x8000 for `__Boot()` function
  - Location: `cmd/corelx/main.go`, `internal/corelx/codegen.go`

### Removed
- **Old CoreLX Documentation Files** - Cleaned up redundant CoreLX documentation
  - Removed 10 old CoreLX status/implementation/guide files
  - Consolidated into `docs/CORELX.md` and `CORELX_DEBUGGING_ISSUES.md`
  - Files: `CORELX_APU_IMPLEMENTATION.md`, `CORELX_COMPILER_STATUS.md`, etc.

### Known Issues
- **CoreLX Blank Screen Issue** - CoreLX-compiled ROMs show blank screen
  - Test ROMs: `moving_sprite_colored_corelx.rom`, `moving_sprite_colored_simple.rom`
  - Possible causes: Variable persistence, `wait_vblank()` loop, OAM writes, tile loading
  - Status: In progress - see `CORELX_DEBUGGING_ISSUES.md` for details
  - Date: 2026-01-30

### Added
- **CoreLX Compiler** - Complete compiler implementation for CoreLX language
  - Lexer, parser, semantic analyzer, and code generator
  - Lua-like syntax compiled to Nitro-Core-DX bytecode
  - Documentation: `CORELX_PROGRAMMING_GUIDE.md`, `CORELX_COMPILER_STATUS.md`
  - Test ROMs and examples in `test/roms/`
  - Location: `cmd/corelx/`, `internal/corelx/`
- **Interactive Debugger** - Debugger tool for ROM development
  - Breakpoints, step execution, register viewing
  - Documentation: `docs/DEBUGGING_GUIDE.md`, `docs/DEBUGGING_QUICK_START.md`
  - Location: `cmd/debugger/`, `internal/debug/debugger.go`
- **ROM Builder Enhancements** - Added `EncodeXOR` function for XOR instruction encoding
  - Location: `internal/rom/builder.go`
- **Input System Update** - Changed ButtonSELECT to ButtonZ for better controller mapping
  - Z key now maps to ButtonZ instead of SELECT
  - Location: `internal/input/input.go`, `internal/ui/ui.go`

### Changed
- **Code Refactoring Rollback** - Rolled back refactoring changes that caused performance degradation
  - Restored original CPU, memory, and emulator implementations
  - Performance restored from ~18 FPS back to ~60 FPS
  - Location: `internal/cpu/`, `internal/memory/`, `internal/emulator/`
  - Date: 2026-01-28
- **Console Mockup Images** - Added prototype design images to README
  - Console isometric view
  - Console top view
  - Controller design
  - Images showcase what the physical console will look like
- **Test Suite** - Comprehensive test coverage for all new features
  - Sprite priority, blending, and mosaic effect tests
  - Matrix Mode outside-screen and direct color tests
  - DMA transfer tests
  - PCM playback tests (basic, loop, one-shot, volume)
  - Interrupt system tests (IRQ, NMI, masking)
  - Test documentation (TEST_SUMMARY.md, TEST_RESULTS.md, TEST_FIXES.md)
- **ROM Compatibility Fix** - Fixed sprite blending backward compatibility
  - Normal mode (blendMode=0) now ignores alpha value
  - Maintains compatibility with ROMs using control byte 0x03
  - Fixed sprite transparency issue in normal mode
- **Matrix Mode Outside-Screen Handling** - Complete outside-screen coordinate handling
  - Repeat/wrap mode (default)
  - Backdrop mode (render backdrop color when outside bounds)
  - Character #0 mode (render tile 0 when outside bounds)
- **Matrix Mode Direct Color Mode** - Direct RGB color rendering
  - Bypass CGRAM palette lookup
  - Direct 4-bit per channel color expansion
  - Per-layer direct color control
- **PCM Playback** - Complete PCM audio playback system
  - PCM channel support (one per audio channel)
  - 8-bit signed PCM sample playback
  - Loop and one-shot playback modes
  - PCM volume control
  - Integrated with existing audio channel system
- **Sprite Blending/Alpha** - Complete sprite blending system
  - Normal, alpha, additive, and subtractive blend modes
  - Alpha transparency (0-15 levels)
  - Sprite-to-background blending
- **Mosaic Effect** - Per-layer mosaic support
  - Configurable mosaic size (1-15 pixels)
  - Pixel grouping for retro/pixelated effects
- **DMA System** - Direct Memory Access for fast transfers
  - Memory to VRAM/CGRAM/OAM transfers
  - Copy and fill modes
  - DMA registers and control
- **Sprite Priority System** - Complete sprite priority sorting and rendering
  - Sprites sorted by priority (bits [7:6] of attributes)
  - Proper sprite-to-background priority interaction
  - Unified priority system (BG3=3, BG2=2, BG1=1, BG0=0, Sprites=0-3)
  - Sprites can render behind backgrounds based on priority
- **Interrupt System** - Complete interrupt handling implementation
  - IRQ/NMI handlers with vector table
  - Interrupt vector table (bank 0, addresses 0xFFE0-0xFFE3)
  - VBlank interrupt (IRQ) automatically triggered
  - Interrupt state saving (PC, PBR, Flags to stack)
  - Non-maskable interrupt (NMI) support
  - Interrupt enable/disable via I flag
- **Per-Layer Matrix Mode** - Each background layer (BG0-BG3) now supports independent Matrix Mode transformations
  - BG1, BG2, BG3 matrix registers (0x802B-0x8051)
  - Multiple simultaneous 3D objects (roads, buildings, boxes, etc.)
  - Per-layer matrix control, center points, and mirroring
- **HDMA Matrix Updates** - Per-scanline matrix parameter updates via HDMA
  - Matrix A, B, C, D, Center X, Center Y can be updated every scanline
  - Enables advanced perspective effects (roads, buildings, 3D landscapes)
  - HDMA table format: 64 bytes per scanline (4 layers × 16 bytes)
- **Enhanced Matrix Mode Capabilities**:
  - Multiple simultaneous transformations (SNES Mode 7 could only do one layer)
  - Per-scanline perspective effects
  - 3D town scenes with multiple transformed objects
  - Independent transformations per layer
- **Timing Synchronization** - Unified clock system with CPU and PPU synchronized at ~7.67 MHz (Genesis-like speed)
  - CPU speed: 7,670,000 Hz (changed from 10 MHz)
  - PPU timing: 220 scanlines × 581 dots = 127,820 cycles per frame
  - APU timing: ~174 cycles per sample (adjusted for new CPU speed)
- **Performance Optimizations** - Improved frame rendering performance
  - Optimized PPU `StepPPU()` to process scanlines in batches instead of dot-by-dot
  - Removed debug logging overhead (SPRITE0_STATE logging removed)
  - Batch stepping for CPU/PPU when cycle logging disabled
  - Performance improved from ~27 FPS to ~35 FPS
- **NitroLang Language Design** - Designed new compiled language with Lua-like syntax
  - Documentation: `docs/LANGUAGE_DESIGN.md`
  - Renamed from "NitroScript" to "NitroLang" (compiled, not interpreted)
  - Features: Lua-like syntax, compiled to bytecode, inline assembly support
- **Timing Analysis Documentation** - Added timing analysis and fix summary documents
  - `docs/TIMING_ANALYSIS.md` - Detailed timing analysis and design decisions
  - `docs/TIMING_FIX_SUMMARY.md` - Summary of timing synchronization changes

### Changed
- **Matrix Mode Implementation** - Fully implemented with per-layer support
  - Matrix Mode status updated from "not fully implemented" to "fully implemented"
  - Legacy Matrix Mode registers (0x8018-0x802A) now map to BG0 for backward compatibility
- **Register Map Updates**:
  - Window registers moved to 0x8052-0x805C (was 0x802B-0x8035)
  - HDMA registers moved to 0x805D-0x805F (was 0x8036-0x8038)
  - New per-layer matrix registers added (0x802B-0x8051)
- **CPU Clock Speed** - Reduced from 10 MHz to ~7.67 MHz (Genesis-like)
  - Target: 127,820 cycles per frame at 60 FPS
  - Better matches Genesis console speed
- **PPU Timing** - Adjusted to match CPU speed
  - Dots per scanline: 360 → 581
  - HBlank dots: 40 → 261
  - Total cycles per frame: 79,200 → 127,820
- **Logging System** - All logging disabled by default
  - Console logging removed unless `-log` flag is used
  - Removed SPRITE0_STATE debug output that was causing performance issues
  - Cycle logging only enabled with `-cyclelog` flag
- **Build System** - Added `-tags no_sdl_ttf` build option
  - Allows building without SDL2_ttf dependency
  - Uses simple bitmap font renderer as fallback
- **Documentation** - Updated Programming Manual with new features
  - Added per-layer matrix register documentation
  - Added HDMA matrix update documentation
  - Added interrupt system documentation (new section)
  - Updated register map with new matrix registers
  - Added examples for multiple simultaneous matrix transformations
  - Added interrupt handler examples

### Fixed
- **Performance Issues** - Removed excessive logging overhead
  - Removed `fmt.Printf` statements that were printing to console every frame
  - Optimized PPU rendering loop
  - FPS improved from ~27 to ~35

### Planned
- Tile Viewer panel for visual VRAM inspection
- Advanced debugging tools (breakpoints, watchpoints)
- Interrupt system implementation
- Matrix Mode (Mode 7-style) transformation
- SDK asset pipeline
- IDE integration
- Further PPU rendering optimizations to reach 60 FPS

---

## [0.2.0] - 2026-01-27

### Added
- **Cycle-by-Cycle Debug Logger** - Comprehensive logging system that records CPU registers, PPU state (scanline, dot, VBlank flag, frame counter), APU state (all 4 channels), and key memory locations for each clock cycle
  - Command-line flags: `-cyclelog <file>`, `-maxcycles <N>`, `-cyclestart <N>`
  - UI toggle: Debug → Toggle Cycle Logging
  - Supports start cycle offset to skip initialization
  - Single file output with all state information
- **Register Viewer Panel** - Real-time CPU register display with:
  - All registers (R0-R7, PC, SP, PBR, DBR, Flags)
  - Scrollable display (fixes off-screen issue)
  - Copy All button for clipboard access
  - Save State button to save register state to timestamped file
  - Binary representation for registers
- **Memory Viewer Panel** - Hex dump viewer with:
  - Bank selector (0-255)
  - Offset selector (0x0000-0xFFFF)
  - 16 bytes per line display
  - ASCII representation
  - Real-time updates
- **PPU State Getters** - Added `GetScanline()` and `GetDot()` methods for debugging
- **OAM/PPU/APU Adapters** - Interface adapters to avoid import cycles in debug logger

### Fixed
- **MOV Mode 2 I/O Register Bug** (Critical) - Fixed sprite movement issue
  - **Problem**: Mode 2 was reading 16 bits from I/O registers (which are 8-bit only)
  - **Impact**: ROMs reading VBlank flag got 0x0100 instead of 0x0001, causing infinite wait loops
  - **Solution**: Automatic detection of I/O addresses (bank 0, offset >= 0x8000) - reads 8-bit and zero-extends to 16-bit
  - **Location**: `internal/cpu/instructions.go:30-49`
  - **Hardware Compatibility**: ✅ FPGA-implementable using standard address decoding logic
- **VBlank Flag Timing** - Improved flag persistence through entire VBlank period
  - Flag now correctly persists through scanlines 200-219
  - Fixed flag re-set logic to ensure it's available throughout VBlank

### Changed
- **MOV Mode 2 Behavior** - Now automatically detects I/O vs normal memory:
  - I/O registers: Reads 8-bit, zero-extends to 16-bit
  - Normal memory: Reads 16-bit as before
- **MOV Mode 3 Behavior** - Already had I/O detection, now consistent with Mode 2
- **Programming Manual** - Updated to document automatic I/O register detection
- **Documentation** - Consolidated and updated all documentation

### Documentation
- Updated `NITRO_CORE_DX_PROGRAMMING_MANUAL.md` to version 1.1
- Documented automatic I/O register detection in MOV instructions
- Updated `MASTER_PLAN.md` with current status
- Updated `README.md` with latest features

---

## [0.1.0] - 2026-01-06 to 2026-01-26

### Added
- **Clock-Driven Architecture** - Complete refactor to cycle-accurate, FPGA-ready design
  - Master clock scheduler coordinating CPU, PPU, and APU
  - PPU scanline/dot stepping for pixel-perfect rendering
  - APU fixed-point audio synthesis
- **Memory System Split** - Separated Bus and Cartridge for better organization
- **Save State System** - Complete save/load state implementation using encoding/gob
- **Logging System** - Centralized logging with component filtering
- **Basic Debugging Tools** - Initial debugger infrastructure
- **Fyne UI Framework** - External UI using Fyne with SDL2 for rendering
- **ROM Builder** - Tools for building test ROMs

### Fixed
- **CPU Reset() Corruption Bug** (Critical)
  - Issue: Reset() set PCBank=0, causing crashes after ROM load
  - Fix: Reset() no longer resets PCBank/PCOffset/PBR
  - Location: `internal/cpu/cpu.go:74-92`
- **Frame Execution Order Bug** (Critical)
  - Issue: VBlank flag set AFTER CPU execution
  - Fix: PPU.RenderFrame() moved before CPU execution (now clock-driven)
  - Location: `internal/emulator/emulator.go`
- **MOV Mode 3 I/O Write Bug** (Critical)
  - Issue: Always wrote 8-bit to I/O, breaking 16-bit writes
  - Fix: Write 16-bit to non-I/O addresses, 8-bit to I/O
  - Location: `internal/cpu/instructions.go:38-53`
- **Logger Goroutine Leak**
  - Issue: Logger goroutine never shut down
  - Fix: Added logger.Shutdown() calls in UI cleanup
  - Location: `internal/ui/ui.go`, `internal/ui/fyne_ui.go`
- **Division by Zero**
  - Issue: Returned 0xFFFF silently
  - Fix: Added FlagD (division by zero flag)
  - Location: `internal/cpu/instructions.go:171-188`
- **Stack Underflow**
  - Issue: Returned 0 without error
  - Fix: Pop16() now returns error on underflow
  - Location: `internal/cpu/instructions.go:501-517`
- **APU Duration Loop Mode**
  - Issue: Didn't reload initial duration
  - Fix: Store InitialDuration, reload on loop
  - Location: `internal/apu/apu.go`

### Changed
- **Architecture**: Migrated from frame-based to clock-driven execution
- **PPU Rendering**: Changed from frame-based to scanline/dot stepping
- **APU Audio**: Migrated to fixed-point arithmetic for FPGA compatibility
- **Memory System**: Split into Bus (routing) and Cartridge (ROM storage)

### Documentation
- Created `SYSTEM_MANUAL.md` - Complete system architecture documentation
- Created `NITRO_CORE_DX_PROGRAMMING_MANUAL.md` - Programming guide for ROM developers
- Created `MASTER_PLAN.md` - Consolidated planning and review document
- Consolidated documentation from multiple files into main documents
- Archived historical documentation to `docs/archive/`

---

## [0.0.1] - Initial Development

### Added
- **Core CPU Emulation** - 16-bit CPU with banked 24-bit addressing
  - 8 general-purpose registers (R0-R7)
  - Complete instruction set (arithmetic, logical, branching, jumps)
  - Cycle-accurate execution
- **Memory System** - Banked memory architecture
  - Bank 0: WRAM (32KB) + I/O registers
  - Banks 1-125: ROM space
  - Banks 126-127: Extended WRAM (128KB)
- **PPU (Graphics)** - Picture Processing Unit
  - 4 background layers (BG0-BG3)
  - Sprite system (128 sprites)
  - VRAM, CGRAM, OAM management
  - 320x200 pixel display
- **APU (Audio)** - Audio Processing Unit
  - 4 audio channels
  - Waveforms: sine, square, saw, noise
  - Duration control with loop mode
  - 44.1 kHz sample rate
- **Input System** - Dual controller support with 12 buttons
- **ROM Loading** - ROM header parsing and execution
- **Basic UI** - Initial user interface

### Known Issues
- Various bugs documented in MASTER_PLAN.md (all fixed in later versions)

---

## Version History Notes

**Version 0.2.0** marks a significant milestone:
- ✅ Sprite movement working correctly
- ✅ Comprehensive debugging tools
- ✅ FPGA-ready architecture complete
- ✅ All critical bugs fixed

**Version 0.1.0** represents the clock-driven refactor:
- Complete architecture overhaul for FPGA compatibility
- Cycle-accurate execution
- Hardware-accurate synchronization signals

**Version 0.0.1** was the initial development phase:
- Core emulation systems implemented
- Basic functionality working
- Foundation for future improvements

---

## Format Notes

- **Added** - New features
- **Changed** - Changes to existing functionality
- **Deprecated** - Features that will be removed
- **Removed** - Removed features
- **Fixed** - Bug fixes
- **Security** - Security fixes

---

## Links

- [README.md](README.md) - Project overview
- [SYSTEM_MANUAL.md](SYSTEM_MANUAL.md) - System architecture
- [PROGRAMMING_MANUAL.md](PROGRAMMING_MANUAL.md) - Programming guide
- [docs/archive/plans/MASTER_PLAN_CONSOLIDATED_2026-01.md](docs/archive/plans/MASTER_PLAN_CONSOLIDATED_2026-01.md) - Development planning
