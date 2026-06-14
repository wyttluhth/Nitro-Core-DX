# NitroPackInDemo → CoreLX Extraction (Milestone 7)

This is the Milestone 7 deliverable from [DESIGN.md](DESIGN.md): a system-by-system
mapping of everything the ROM-first demo (`build_rom.go`) does, to the CoreLX API
that should replace it, with an honest gap analysis against the compiler as it
exists today (`internal/corelx`).

Guiding rule (from DESIGN.md): the lesson is **not** "CoreLX should expose raw
register writes." Common game tasks get ergonomic high-level helpers; advanced
work keeps low-level escape hatches.

How to read each section:

- **ROM today** — what `build_rom.go` actually does, with line references
- **Proposed CoreLX** — the API surface the M8 rebuild should target
- **Exists today** — what the current compiler already provides
- **Gap** — `none` / `partial` / `missing`

---

## 1. Asset import and bitmap matrix planes

**ROM today** (`build_rom.go:483-490, 608-631`): PNGs are loaded in Go and
converted via `emulator.BuildBitmapMatrixPlaneAssetFromImage` into bitmap
matrix-plane assets (bitmap data + quantized 15-color palette), then placed in
ROM data banks (`allocateROMData`, `appendDataBlob`) and DMA-uploaded at boot
(`emitUploadPlaneBitmap`, `emitMatrixBitmapDMAChunks`). Placeholder art (NPC,
interior floor) is generated procedurally in Go (`buildNPCImage`,
`buildInteriorFloorImage`). Billboard images are auto-cropped and
bottom-anchored (`normalizeBillboardImage`, lines 341-400).

**Proposed CoreLX**

PNG import is an **editor-time** operation, not a compile-time one: the
devkit importer converts the PNG once (quantization, billboard
crop/bottom-anchor) and writes the result into the game file as a text
asset section. The compiler only ever parses text.

```corelx
-- ======== backgrounds ========
background ParkFloor:
    kind: bitmap_plane
    plane_size: 128          -- 32/64/128
    palette: hex 0000 7FFF ...   -- baked at import time
    data: hex
        ...

function Start()
    matrix_plane.load_bitmap(ParkFloor, 0)   -- plane ctl + flags + palette + DMA in one call
```

- The importer reuses the existing
  `emulator.BuildBitmapMatrixPlaneAssetFromImage` conversion code, moved
  into the devkit import path.
- `matrix_plane.load_bitmap(asset, channel)` uploads bitmap + palette + sets
  plane control/flags, hiding the DMA chunk loop (the ROM does palette CGRAM
  writes manually at lines 678-689).

**Exists today**: `tiles8/tiles16/sprite/tileset/tilemap` asset kinds with
inline hex only ([assets.go](../../internal/corelx/assets.go)); external
manifests via `corelx.assets.json`. No image files, no bitmap kind, no
palette generation.

**Gap: missing** — this is the single biggest blocker for M8 and should be
built first; nothing else can render without assets in the ROM.

---

## 2. Pseudo-3D matrix floor (projection + camera)

**ROM today** (`build_rom.go:734-746`): writes the matrix-plane projection
register block — projection mode (`0x8091`), horizon (`0x8092`), camera X/Y
(`0x8093-96`), heading X/Y (`0x8097-9A`), base distance (`0x809B`), focal
length (`0x809D`), width scale (`0x809F`). Per-frame camera/heading sync is
`emitSyncPlaneCameraHeading` (lines 435-441). The floor camera trails the
player by `heading >> 2` so turning pivots at the feet (lines 1238-1250).

**Proposed CoreLX**

```corelx
-- tier-1 builtins, mirroring the hardware registers 1:1, identical for
-- all four plane channels and all projection modes
matrix_plane.set_projection(channel, mode, horizon)   -- mode: 0 none, 1 perspective rows, 2 vertical quad
matrix_plane.set_projection_depth(channel, base_distance, focal_length, width_scale)
matrix_plane.set_camera(channel, x, y, heading_x, heading_y)
```

Conveniences layered on these (surface presets, camera-follow styles) belong
to game code and optional modules.

**Exists today**: `matrix_plane.enable/load_tiles/load_tilemap/set_tile/...`
cover the 2D plane memory only. None of the projection/camera registers are
exposed; today they would require dozens of 8-bit `mem.write` calls per frame.

**Gap: missing** — the register-level builtins are required for M8.

---

## 3. Vertical billboards (building facade, NPC)

**ROM today** (`build_rom.go:53-70, 416-433, 634-674`): a 13-field
`billboardPlane` config struct per object — plane channel, BG layer binding,
projection params, world origin X/Y, facing vector, width/height scale.
`emitConfigureVerticalBillboardPlane` writes vertical projection mode plus
origin (`0x80A1-A4`), facing (`0x80A5-A8`), height scale (`0x80A9`). Billboards
sync to the **raw player position** (not the feet-pivot camera) so scale stays
correct (lines 1252-1256).

**Proposed CoreLX**

```corelx
-- tier-1 builtins for the vertical-projected-quad register block
-- (0x80A1-0x80A9): world surface placement, hardware vocabulary only
matrix_plane.set_surface(channel, origin_x, origin_y, facing_x, facing_y)
matrix_plane.set_surface_scale(channel, width_scale, height_scale)
```

The ROM's most error-prone pattern — keeping plane channel, BG layer
control, transform bind, and matrix control consistent per world object
(lines 636-674 hardcode four parallel register addresses per object) — is
real, but the fix is an optional module or per-game helper struct, not a
language concept. The demo rebuild should first write it as plain game
code; promote to a module only if a second game wants it.

**Exists today**: nothing for vertical projection.
**Gap: missing** (same register family as §2; build together).

---

## 4. Scene system

**ROM today** (`build_rom.go:497-502, 825-836`): scene ID + return-scene ID in
WRAM; the main loop reads the scene var and branches to one of six scene
blocks; every scene ends with a jump back to `main_loop`. Pause is a scene
that remembers where to return (`wramSceneReturn`). Transitions write the
scene var and reset per-scene state by hand (e.g. door entry resets interior
position/heading, lines 1288-1298; credits reset everything, lines 1463-1478).

**Proposed CoreLX** — the game-state-machine pattern, taught in the manual
(§13):

```corelx
var scene: int = SCENE_TITLE

function Start()
    while true
        wait_vblank()
        input.poll()
        if scene == SCENE_TITLE
            update_title()
        else if scene == SCENE_OVERWORLD
            update_overworld()
        ...
```

Transitions are one assignment (`scene = SCENE_CREDITS`). The pattern needs
only globals and constants from the language core.

**Exists today**: user-defined functions, `if/while`, structs. The dispatch
pattern is writable now; clean state sharing is not.
**Gap: partial** — needs globals and `const` (§11); nothing else.

---

## 5. Input polling and edge detection

**ROM today** (`build_rom.go:816-823`): per-frame latch strobe (`0xA001`),
read low/high input bytes, store to WRAM. Edge detection (act on press, not
hold) is hand-rolled **six separate times** — title START, pause START,
interior pause, door A, NPC-talk A, exit A — each with its own held-flag WRAM
slot and 10 instructions of branch logic (e.g. lines 847-863).

**Proposed CoreLX**

```corelx
input.poll()                       -- latch + read, once per frame
if input.held(UP) ...              -- level
if input.pressed(A) ...            -- rising edge (held-flag managed internally)
if input.released(START) ...
```

Button-name constants (`UP/DOWN/LEFT/RIGHT/A/B/START/...`) as built-in
constants, replacing the raw masks (`0x0001`, `0x0004`, `0x0010`).

**Exists today**: `input.read(controller)` returns the raw word; everything
else is user code.
**Gap: partial** — `pressed/released` need per-button previous-state storage in
the runtime (compiler-reserved WRAM), which is exactly the kind of hidden
state a language runtime should own. This pattern was the demo's most
repeated boilerplate; high payoff, small surface.

---

## 6. Heading-based movement

**ROM today** (`build_rom.go:116-143, 933-1013`): a 64-entry heading table in
WRAM (cos, sin, move_x, move_y per entry, 8.8 fixed point, generated at build
time); Left/Right turn the heading index (rate-limited to every 4th frame via
`turnTick`), Up/Down add/subtract the move vector. The same ~80-instruction
block appears twice (overworld + interior) with different state addresses.

**Proposed CoreLX**

This is **demo game code**: a `Walker` struct and an update function written
inside the demo cartridge, using `const` data for the heading table, `fixed`
math, and `input.held(...)`. The heading table is text data in the cartridge
(generated once by a devkit tool, consistent with import-time conversion).
What v1 must supply is only the language: globals, `const` arrays, `fixed`
arithmetic, structs by reference. If a second game wants the same scheme,
that is the promotion trigger for a devkit module — not before.

**Exists today**: nothing; expressible in user code once arrays/data tables
exist.
**Gap: partial** — blocked only on language core (§11); no builtins, no
module.

---

## 7. Bounds, collision, trigger zones

**ROM today**: axis-aligned clamps and zone tests, all hand-written compares:

- world bounds clamp (lines 1197-1215), room bounds clamp (995-1013)
- building facade stop: X-range + Y-clamp (1217-1227)
- NPC block: X-range + Y-clamp (1015-1023)
- door / talk / exit trigger zones: X/Y range + `A` edge (1271-1298, 1060-1115)

**Proposed CoreLX**

All of it is **demo game code**: plain `if` + comparison functions over
struct data. The only format-level concept retained is generic: background
assets may declare **named regions** (`region door 494 530 600 696`) that
compile to ROM data tables — pure named rectangles whose *meaning* (solid,
trigger, damage, anything) is assigned by game code. No `collide`/`zones`
devkit module in v1; promotion trigger is a second game wanting one.

```corelx
if region.contains(ParkFloor.door, player.x, player.y) and input.pressed(A)
    scene = SCENE_INTERIOR
```

(`region.contains` is the one candidate helper — and even it can start as a
plain function in the demo.)

**Exists today**: `if` + comparisons cover all of it once structs can be
passed around (they can).
**Gap: partial** — blocked only on language core; no builtins, no module.

---

## 8. Text and HUD

**ROM today** (`build_rom.go:82-90, 1369-1388`): the text port
(`0x8070-0x8076`) — 16-bit X, Y, RGB color, then one char at a time.
`emitText`/`emitTextCentered` wrap it; centering is `(320 - len*8)/2`
computed at build time. Note: Shmup1 was forced to hand-roll the same
wrappers via raw `mem.write` ([main.corelx:110-133](../Shmup1/main.corelx)) —
a symptom of the gap, and one of the patterns that made that code brittle.

**Proposed CoreLX**

```corelx
text.draw(8, 8, rgb(248, 248, 248), "NITRO PACK-IN DEMO")
text.draw_centered(98, rgb(255, 255, 255), "PRESS START")
```

Requires **string literals** (new lexer/parser/codegen work: string data in
ROM, emit loop or unrolled writes). 16-bit X write must be handled correctly
(two byte writes — the exact thing `mem.write` can't express, §11).

**Exists today**: nothing; no string type at all.
**Gap: missing** — second-highest priority after assets; every scene draws text.

---

## 9. Dialogue system (typewriter)

**ROM today** (`build_rom.go:526-530, 593-599, 714-718, 1332-1437`): page
texts stored as one 16-bit word per character in WRAM (so the reveal loop can
stream with indexed loads); reveal counter +1 per frame; `A` skips to full
reveal, then advances pages, then transitions to credits; "PRESS A" prompt
when a page completes.

**Proposed CoreLX**

**Demo game code**: a `Dialog` struct, page strings, and an update function
written in the demo cartridge from `text.draw` + `input.pressed` + arrays.
What v1 must supply is only language core plus `text.draw`. A devkit
`dialog` module is a *post-v1 promotion candidate* once a second game wants
this presentation — likely, given the owner's adventure-game plans, but it
earns its way in with evidence, not by inheriting this demo's shape.

**Exists today**: nothing (needs strings §8, arrays §11, input edges §5).
**Gap: partial** — pure composition of language core + §5 + §8; no builtins,
no module.

---

## 10. Sprites, layer control, frame loop, credits

Mostly covered by existing builtins; small deltas:

| ROM behavior | Lines | Existing CoreLX | Delta |
|---|---|---|---|
| Player sprite (fixed screen pos, priority over BG) | 532-538, 1258-1260 | `Sprite()`, `sprite.set_pos`, `oam.write`, SPR helpers | none |
| Clear sprite | 200-208 | `oam.clear_sprite` | none |
| Per-scene layer enable/disable sets | 443-481 | `bg.enable/disable`, `bg.bind_transform`, `matrix.enable` | partial: ROM also writes plane bind (`0x806C-6F`) and composite control values; verify each register has a builtin equivalent, add the missing ones |
| Wait one frame (frame-counter compare) | 91-93 | `wait_vblank()`, `frame_counter()` | none |
| Direct VRAM tile write at boot | 145-157, 695 | `gfx.load_tiles` with asset | none (use asset) |
| CGRAM palette set | 88-90, 678-693 | `gfx.set_palette_color` | none |
| Credits full state reset | 1443-1480 | plain code | none once globals exist; `scene.goto(Title, reset: true)` as sugar later |

**Gap: none/partial** — audit the layer-control registers during M8 and fill
holes as they surface.

---

## 11. Language-level pain points (observed, not speculative)

These came directly from writing/reading the ROM and Shmup1 workarounds:

1. **No global variables.** A program is assets + types + functions
   ([parser.go:36-38](../../internal/corelx/parser.go)); all cross-function
   state goes through hand-assigned WRAM addresses via `mem.read/write`
   (Shmup1 does exactly this). The demo has 16 named WRAM slots
   (`build_rom.go:504-520`). Proposal: top-level `var` declarations with
   compiler-assigned WRAM addresses; struct-typed globals included.
2. **No arrays / data tables.** Heading table (64×4 entries), starfield
   (Shmup1), dialogue pages — all hand-addressed WRAM. Proposal: fixed-size
   arrays over WRAM plus read-only data tables in ROM banks.
3. **No string literals.** Required by §8/§9.
4. **No named constants.** `build_rom.go` has ~60 Go `const`s (world bounds,
   zone edges, register values). CoreLX programs would inline magic numbers.
   Proposal: `const` declarations (compile-time fold, no storage).
5. **`mem.write` is 8-bit only.** The projection registers are 16-bit pairs;
   the docs explicitly warn about this. Proposal: `mem.write16/read16`, plus
   making the §2/§3 builtins handle widths internally.
6. **Fixed-point convention.** Everything camera-related is 8.8; the language
   has no support or even naming convention. Minimum: document it and add
   `FP(1.5)`-style compile-time literals; full fixed-point types can wait.
7. **Stale docs.** [docs/CORELX.md](../../docs/CORELX.md) lists user-defined
   functions as "Planned" — they are implemented and already used by existing
   `.corelx` sources. Fix during M8 so the docs can be trusted as the
   language reference.

A note on Shmup1 as evidence: it is cited here only as a record of what the
current language *forces* authors to do (raw `mem.*` state, hand-rolled text
wrappers), not as a quality reference. Its weaknesses largely trace back to
these same gaps, which strengthens the case for fixing them at the language
level rather than per-game.

---

## 12. Build-order recommendation for M8

Dependency-ordered; each step is independently testable against the ROM
reference:

1. **Language core**: globals, constants, arrays, string literals,
   `mem.read16/write16` (§11) — everything else leans on these
2. **Image assets + bitmap planes** (§1) — unblocks rendering parity
3. **Projection/camera/billboard builtins** (§2, §3 low-level)
4. **`input.poll/held/pressed`** (§5)
5. **`text.draw`** (§8)
6. **Genre-neutral devkit modules written in CoreLX itself**: `anim`
   (sprite animation over OAM builtins) and `sfx` — these validate the
   module system without baking any game's design in. Walker, dialogue,
   and collision (§6, §7, §9) are **demo game code**, not modules; each is
   a post-v1 promotion candidate only when a second game proves the reuse.
7. **Rebuild demo as `main.corelx`**, validate with the existing
   frame-capture test approach (`build_rom_test.go` scene-flow tests as the
   behavioral spec)

The split in step 6/7 is the design fulcrum: if the walker, dialogue, and
collision systems can be written *as plain CoreLX game code* and feel good,
the language is doing its job. Wherever they can't, that's a missing
language feature — surface it and decide deliberately rather than
reflexively adding another builtin or module.

---

## 13. Design decisions (settled 2026-06-12)

Decided with the project owner during the M7 design discussion:

**Memory model — hybrid auto-allocation with pins.**
Top-level `var` declarations get compiler-assigned WRAM addresses. The
compiler reserves a runtime block first (e.g. `input.pressed()` previous-frame
state), then allocates globals, and emits a memory-map listing (name →
address) with every build for the debugger. A documented user scratch region
is guaranteed never compiler-touched, so raw `mem.*` code stays safe.
`var name at 0xNNNN: type` pins hardware-sensitive buffers (DMA targets,
streamed tables); pins are overlap-checked against the auto region.
Forward-compatible with save-RAM (`persistent var` → different region).

**Modules — devkit-installed, enabled per-project via directive header.**
Modules are plain `.corelx` files installed in the devkit's `modules/`
folder (readable, copyable, user-writable — extensibility is structural).
A project enables them through a directive header in the main file, written
and maintained by the editor's "add module" action and parsed by the
compiler:

```corelx
--! corelx 0.9
--! modules: walker, dialog, zones
```

`--!` lines are directives, not comments: legal only at the top of the main
file, enforced at compile time ("module `dialog` not installed" instead of
"unknown function"). One file fully describes the game — no sidecar
manifest. Module functions are namespaced by module name (`walker.update`),
matching builtin syntax, so features can migrate between builtin and module
without changing game code.

**Scenes — documented pattern, not a keyword and not a module.**
The "scene system" in the ROM is a classic game-state machine: a state
variable plus a dispatch branch in the main loop. Once globals exist, that
is ~10 lines of plain CoreLX (`var scene` + `if/else if` over scene update
functions); a module would only wrap one assignment in a function call.
Scenes are taught as a recipe in the programming manual, with the rebuilt
demo as the living example. If real scene logic emerges later (scene
stacks, transition effects), a module can exist then. §4 shows the
pattern; §12 step 6 lists the v1 modules.

**Single-file game format — sections for code and text-encoded data.**
A v1 game is exactly one text file in, one ROM binary out (PICO-8 cartridge
model). The file has sections separating code from data: `sprites`,
`backgrounds`, `audio`, with all asset data represented as text. Two honest
text tiers: *semantic* text (animations, collision metadata, music notes —
hand-editable, meaningfully diffable) and *hex blobs* (pixel/sample data —
still text and diffable, edited through tools). Specifics:

- **Import-time conversion**: devkit editors (sprite editor, image importer,
  future tracker) convert binary media → canonical text once, at import. The
  compiler only parses text, so its asset handling is trivially
  deterministic and freezable (same file → same ROM, byte for byte,
  forever), while importers can improve freely — better quantization only
  affects future imports, never existing games.
- **Sprites own frames and named animations** (`anim walk_up: 1 2 3 4`,
  `anim walk_right: mirror_h walk_left` — mirroring references data instead
  of duplicating it, via the OAM flip bit). Code references animations by
  name; the compiler emits ROM tables.
- **Backgrounds own metadata**: named `region` rectangles compile to
  queryable ROM tables (`ParkFloor.door`). Regions are pure named data —
  their meaning (solid, trigger, damage, anything) is assigned by game
  code, never by the format. The metadata schema may grow additively.
- **Audio is text too**: tracker-style note/pattern sections for music,
  named parameter blocks for instruments/FM patches, hex for raw samples.
- **Editor round-trip contract**: devkit tools must parse, edit, and rewrite
  their section losslessly without disturbing code or comments — the file
  is the database.
- Accepted tradeoff: large data sections make long files (a 256×256 bitmap
  plane ≈ 64KB of hex). Post-v1, *additive* relief valves are allowed
  (external file references, sprite sheets); v1 is strictly single-file.

**Hardware-accuracy contract (authority = the emulator implementation).**
CoreLX codegen targets the *current emulator implementation* (`internal/cpu`,
`internal/ppu`, `internal/memory`), which is the authoritative model the FPGA
core is built to match — not the spec docs, which may have drifted. Two
standing rules for every feature:

1. **Only emit instructions/modes the emulator actually implements**, verified
   by reading `internal/cpu/instructions.go` (not the spec table). Every
   opcode+mode CoreLX generates is audited against that source.
2. **Never depend on a Go-language artifact** — only on operations a 16-bit
   hardware datapath performs. Audited equivalences in use: 16-bit register
   wraparound = two's-complement ALU; `MUL` low-16 truncation; `SAR` (SHR
   mode 3) arithmetic vs `SHR` mode 1 logical; unsigned `DIV`. Each was
   cross-checked emulator↔FPGA Verilog and found identical.
3. **Compile-time folding must equal runtime computation bit-for-bit.** Caught
   and fixed a real divergence: negative `fixed` const products folded toward
   -inf while the runtime `__fixmul` rounds toward zero. The folder now
   mirrors the hardware routine; `hwaccuracy_test.go` pins them together.

This contract is why every language feature lands with an emulator-executed
test (compile -> ROM -> run on the emulator core -> assert machine state),
not just a "compiler makes sense" check.

**Design lens for remaining questions:** the compiler owns tedious
bookkeeping; the language keeps a clearly-marked manual override; the
boundary between them is documented and tooling-visible. Every proposed
feature lands in exactly one of three tiers:

1. **Builtin** — it talks to hardware registers
2. **Module** — it encapsulates non-trivial logic a game would otherwise
   copy-paste, *proven by more than one game wanting it* (v1: `anim`,
   `sfx`; candidates awaiting a second game: walker, dialog, regions)
3. **Pattern/docs** — it is a shape of code; teach it, don't wrap it
   (scenes, credits reset)

**Fixed-point — full `fixed` type, in v1.**
The language ships with a real 8.8 `fixed` type: fixed literals (`fp(1.5)` →
`0x0180` at compile time), automatic `>>8` normalization after multiplies,
and type errors on unconverted `fixed`/`int` mixing. Development is staged
*inside* M8 (literals first, walker with explicit shifts to learn the
requirements, then the full type, then walker rewritten on it) — but no
intermediate state is released. Rationale: shipping convention-based math in
v1 and a typed version later would leave two idioms in the ecosystem
permanently.

**Escape-hatch policy — settled by the tier test.**
Every hardware register gets a tier-1 builtin, by definition. `mem.*` covers
registers that don't have builtins *yet*; helpers and modules layer on top
and never replace the register-level surface.

**Stability contract — v1 freezes semantics; evolution is additive only.**
Project owner's goal: after v1, only bugfixes and compiler optimizations —
ideally no language changes at all. Made precise: *semantic* changes
(altering what existing syntax means, removing features, changing type
rules) are forbidden after v1; *additive* changes (new register builtins for
new hardware, new devkit modules, new asset kinds) are permitted because
existing programs cannot reference them and therefore cannot break. The
keepable promise: *every program that compiles on v1 compiles and behaves
identically on every later version.* Consequence: everything load-bearing
must be settled and proven by the M8 demo rebuild before the freeze — the
remaining open questions below are pre-v1 requirements, and the DESIGN.md
API list (music playback, sprite animation, etc.) needs an explicit
in-or-out call for v1.

**Syntax — see the v1 Syntax Charter.**
All surface-syntax decisions (declarations, numeric types with decimal
literals as `fixed`, structs as reference types with `&` removed, BASIC
`for ... to` loops, data declarations as the section grammar, no `ASSET_`
prefix) are recorded in
[docs/specifications/CORELX_SYNTAX_V1.md](../../docs/specifications/CORELX_SYNTAX_V1.md),
decided 2026-06-12 with learnability as the stated criterion (anchors:
Lua, BASIC, Go). The cartridge format draft is
[docs/specifications/CORELX_CARTRIDGE_FORMAT.md](../../docs/specifications/CORELX_CARTRIDGE_FORMAT.md).
Two earlier sketches in this log are finalized by the charter: struct
reference semantics (charter D8: structs are reference types) and fixed-point
literals (charter D4: decimal literals are `fixed`).

**Generic transformation planes.**
The four matrix planes are general-purpose transformation planes. Tier-1
builtins mirror the hardware's own vocabulary (perspective row projection,
vertical projected quad — `ppu.go:216-256`) and behave identically across
all four channels and all projection modes. Games compose these primitives
into floors, ceilings, world objects, split-screen effects — whatever the
design calls for. Surface treatments above the horizon line are a PPU
capability to confirm with a test ROM; if the hardware grows new projection
modes, they arrive as additive builtins.

**v1 completeness list (settled 2026-06-12).**
In v1: language core (syntax charter), all tier-1 register builtins
(including §2/§3 generic plane projection), `input.poll/held/pressed`,
`text.draw`, image/sprite/background asset formats, **music with a fuller
API** (play/stop/loop plus volume, fade, and jingle-interrupt — owner chose
the larger surface over the minimal one; co-design the playback API with
the audio text format, CORELX_CARTRIDGE_FORMAT.md §5), **sprite animation
as a module** (advances frames via existing OAM builtins), **minimal `sfx`
module** (`sfx.play(name)` on a free channel), plus the scene/credits
documented patterns. Movement, dialogue, and collision are game code; the
demo cartridge doubles as the teaching example for all three, and each
becomes a devkit module when a second game proves the reuse. Out of v1
(additive later): external file
references, sprite sheets, string data type, save/load, and any
plane-projection capability the hardware does not already have.

**System coherence test.** Every piece of the system has exactly one home:
hardware mechanisms are builtins, multi-game logic is modules, and each
game's design lives in its own cartridge. The M8 acceptance test for the
language is behavioral: rebuild the demo in CoreLX and compare outputs
against the ROM reference frame by frame.

## 14. Open questions

None. All design questions raised by this extraction are settled; §13 is
the decision record. Remaining work is specification detail (audio format
grammar with the music API) and implementation (M8 build order, §12).
