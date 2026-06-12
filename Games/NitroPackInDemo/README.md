# NitroPackInDemo

This folder is the canonical home for the ROM-first pack-in demo project.

The full design and build plan lives in:

- [DESIGN.md](/home/aj/Documents/Development/Nitro-Core-DX/Games/NitroPackInDemo/DESIGN.md)

Current status:

- ROM-first implementation path is active
- `build_rom.go` builds the complete demo loop: title, overworld, interior room, NPC dialogue, and credits (milestones M1-M7)
- `build_rom_test.go` covers the full scene loop, movement, bounds, NPC collision, and plane camera-model assumptions
- Milestone 7 (CoreLX design extraction) is documented in [CORELX_EXTRACTION.md](CORELX_EXTRACTION.md)
- Next step is the M8 CoreLX rebuild, starting with the language-core gaps listed there

## Build

From the repository root:

```bash
go run -tags testrom_tools ./Games/NitroPackInDemo -out roms/nitro_pack_in_demo.rom
```

Then run it with:

```bash
./emulator -rom roms/nitro_pack_in_demo.rom
```

Default placeholder assets:

- Floor: `Games/NitroPackInDemo/park.png`
- Billboard/building: `Games/NitroPackInDemo/building.png`

Optional overrides:

```bash
go run -tags testrom_tools ./Games/NitroPackInDemo \
  -floor Games/NitroPackInDemo/park.png \
  -billboard Games/NitroPackInDemo/building.png \
  -out roms/nitro_pack_in_demo.rom
```

## Current Scope

The current ROM covers the complete demo loop:

- Title scene with `PRESS START`
- Overworld pseudo-3D slice using `park.png` as the floor
- Main building facade from `building.png`
- Interior room scene: procedural checkered floor (matrix plane 2) plus an NPC
  guide billboard (matrix plane 3), with room-bounds clamping and NPC collision
- Two-page typewriter dialogue scene with the guide (`A` skips the reveal,
  then advances pages)
- Credits scene; `START` resets all state and returns to the title
- Explicit scene state in WRAM
- Input polling with start/action edge handling
- Matrix floor + vertical projected quad facades, indoors and out
- Open park-floor walk bounds with world clamps at map edges
- Generic matrix-floor movement: `Up/Down` move, `Left/Right` turn
- Centered placeholder player sprite in both walkable scenes
- Pause overlay on `START` while in the interior
- Door trigger into the interior and exit zone back to the overworld on `A`
- Automated full-loop scene-flow and camera-model tests in `build_rom_test.go`

## Active Tuning Focus

The main open rendering issue is the overworld building facade.

Ground anchoring is substantially improved, but the next pass still needs to
make the facade feel less like a camera-facing sprite and more like a properly
turning/foreshortening vertical surface when viewed from an angle.

That work is currently being validated in both:

- `internal/ppu/scanline.go`
- `internal/ppu/features_test.go`

It does not block the demo loop, which is feature-complete; improvements there
land as pure rendering upgrades under the same ROM.
