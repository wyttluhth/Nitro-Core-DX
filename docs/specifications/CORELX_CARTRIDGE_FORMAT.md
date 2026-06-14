# CoreLX Cartridge Format — DRAFT for review

Status: **draft, not yet approved** — written 2026-06-12 as part of the M7/M8
language design work. Decision context lives in
[CORELX_EXTRACTION.md §13](../../Games/NitroPackInDemo/CORELX_EXTRACTION.md).
Syntax marked `(open)` is pending the syntax design discussion.

## 1. Principles

1. **A game is one main text file plus side asset files for heavy art.**
   The main `.corelx` holds code and small inline data; large binary art
   (bitmap matrix planes, audio samples) lives in separate `.cxasset` text
   files referenced by one line. The compiler's output is still a single ROM
   binary. *(Revised 2026-06-14: the original "single file = whole game" rule
   does not survive real images — a single 256×256 floor is ~105 KB of hex,
   and a full-resolution one is ~1.6 MB. See §1a.)*
2. **The compiler only parses text.** All binary→text conversion (PNG
   import, sample import) happens once, at import time, inside the devkit
   importer, which writes `.cxasset` text files. Compiler output is
   deterministic forever: same files → same ROM, byte for byte.
3. **Two text tiers.** *Semantic* text (code, animations, region
   metadata, music notes) is hand-editable and meaningfully diffable. *Hex
   blobs* (pixels, samples) are tool-generated `.cxasset` files, still text
   and diffable but never hand-edited.

### 1b. Project container: the .ncdx package (decided 2026-06-14)

A game project is distributed and seen as a **single file**, `MyGame.ncdx`,
which is internally a ZIP archive (the cross-platform equivalent of a macOS
bundle — one icon, a folder inside). It contains the main `.corelx`, the
`.cxasset` files, and a `project.toml` metadata file.

- **Container-always.** The Studio only ever shows the single `.ncdx`; it
  manages the internals transparently (extract to a working area to edit,
  repack on save). Users never see the loose files unless they deliberately
  rename `.ncdx` -> `.zip` and unzip (the "admin boss" manual-edit path,
  documented in the Programming Guide).
- **Compiler accepts both** a `.ncdx` container (extracted to a temp dir) and
  a plain project folder, so CI, git, and power-user/diff workflows still work
  on the loose files.
- **Compiled output is a `.cart`** ROM (the cartridge the emulator runs).
- **Validation before compile is bidirectional and blocking:** every code
  reference must resolve to a file in the project (missing -> error), and every
  `.cxasset` in the project must be referenced by code (orphan -> error).

### 1a. The hard split (decided 2026-06-14)

A clean, teachable rule decides where each thing lives — no size threshold:

| Lives **inline** in the main `.corelx` | Lives in an external `.cxasset` file |
|---|---|
| code (functions, globals, consts) | bitmap matrix-plane images |
| small sprite/tile pixel data | audio samples |
| collision/region metadata | |
| music notes, instrument params | |

External assets are referenced by one line and used by name:

```corelx
asset ParkFloor: image "assets/park_floor.cxasset"

function Start()
    matrix_plane.load_bitmap(ParkFloor, 0)
```

The importer (`corelx_import`) converts a PNG into the `.cxasset` text file;
the compiler reads that file at build time, places its bitmap in ROM, and the
`load_bitmap` builtin DMAs it onto the plane. Conversion is frozen in the
importer, so builds stay deterministic regardless of where the text lives.
4. **Editors round-trip losslessly.** A devkit tool may rewrite only its own
   section and must preserve everything else — code, comments, ordering.
   The cartridge file is the database.
5. **Canonical formatting.** Tools emit hex in a fixed canonical form
   (lowercase, fixed grouping, fixed line width — exact form TBD) so
   tool-written sections produce minimal diffs.

## 2. File layout

```corelx
--! corelx 1.0
--! modules: anim, sfx
--! title: Nitro Pack-In Demo        -- (open) optional cart metadata

-- code lives at the top level, before any data section

function Start()
    ...

-- ======== sprites ========         -- (open) section marker style

sprite PlayerGuy:
    ...

-- ======== backgrounds ========

background ParkFloor:
    ...

-- ======== audio ========

music TitleTheme:
    ...
```

Rules:

- `--!` directive lines: only at the top of the file, before any code.
  Parsed by the compiler (version check, module enablement). Unknown
  directives are errors (reserves the namespace for additive growth).
- Code section: everything before the first data section. Functions,
  types, constants, global `var` declarations.
- Data sections: each contains only declarations of its kind. Order of
  sections is free; duplicate asset names anywhere in the file are errors.
- Identifiers declared in data sections are referenced from code directly
  by name (`ParkFloor`, `PlayerGuy.walk_up`). No `ASSET_` prefix `(open)`.

## 3. `sprites` section

```corelx
sprite PlayerGuy:
    size: 16                    -- 8 or 16 (pixels per side)
    palette_bank: 1             -- CGRAM bank 0-15
    palette: hex 0000 7fff 03ff ...   -- ≤15 colors + transparent index 0
    data: hex                   -- frames in order, one block per frame
        -- frame 1
        00 00 11 11 ...
        -- frame 2
        ...
    anim idle:       1
    anim walk_up:    1 2 3 4
    anim walk_down:  5 6 7 8
    anim walk_left:  9 10 11 12
    anim walk_right: mirror_h walk_left
```

- Frame indices are 1-based and refer to `data` blocks in order.
- `anim <name>: <frame list>` defines a named animation. Frame rate and
  looping are runtime concerns (the `sprite.play`/animation builtin or
  module), not data-format concerns; a default `rate:` field may be added
  additively later.
- `mirror_h <anim>` / `mirror_v <anim>` reference another animation's
  frames with the OAM flip bit set — no duplicated pixel data.
- Compiler output: pixel data in ROM banks; per-sprite frame/anim tables;
  named constants (`PlayerGuy.walk_up`) usable wherever integers are.

## 4. `backgrounds` section

```corelx
background ParkFloor:
    kind: bitmap_plane          -- bitmap_plane | tilemap | tiles
    plane_size: 128             -- bitmap_plane: 32 | 64 | 128
    palette_bank: 1
    palette: hex 0000 7fff ...
    regions:
        region wall 472 552 600 680     -- name min_x max_x min_y max_y
        region door 494 530 600 696
    data: hex
        ...
```

- `kind` selects the target hardware path: dedicated matrix-plane bitmap,
  BG tilemap, or raw tile patterns.
- `regions` entries compile to ROM tables of named rectangles
  (`ParkFloor.door`). Regions are **pure named data**: the format assigns
  no meaning (solid, trigger, damage — that's game code's call). The
  schema may grow additively (e.g. point markers, paths).
- Import metadata (source filename, import settings) MAY be preserved as
  ordinary comments by the importer, for the author's reference only.

## 5. `audio` section

```corelx
instrument Lead:
    kind: fm                    -- fm | psg | sample
    -- named parameter block; exact FM parameter set mirrors the
    -- YM2608-derived hardware registers (TBD with audio builtins)

music TitleTheme:
    tempo: 120
    channel 0: Lead
        C4 8  E4 8  G4 4  rest 8  ...
    channel 1: psg_square
        ...

sample DoorThunk:
    rate: 8000
    data: hex
        ...
```

- Note events: `<note><octave> <duration>` with `rest`; tracker-style rows
  are an acceptable alternative `(open — decide with audio builtins)`.
- This section is intentionally the least specified: it must be designed
  together with the APU/FM CoreLX API, which is still an open builtin
  surface. The format commitment is only: *music and instruments are
  semantic text; samples are hex.*

## 6. Compiler obligations

- Reject: unknown directives, unknown section kinds, unknown fields
  (additive growth happens by compiler version bump, declared in `--!
  corelx`), duplicate names, dangling references (anim frames out of
  range, region names reused).
- Emit (per build): the ROM, the WRAM memory-map listing (see memory-model
  decision), and a deterministic asset layout.
- Never transform pixel/sample data beyond packing documented in the
  hardware manuals. No quantization, no resampling — that already happened
  at import.

## 7. Post-v1 (additive only, recorded for intent)

- External file references / sprite sheets as *optional alternatives* to
  inline data.
- Additional region schema entries; animation `rate:`/`loop:` fields.
- New section kinds (e.g. `tables` for raw data tables) — though `const`
  arrays in code may cover this in v1.
