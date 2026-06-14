# Documentation Map (Current + Historical)

This directory contains the project's active documentation, historical reviews, and archived planning notes.

This file is the primary navigation entry for docs maintenance.

## Current Sources of Truth (Read These First)

- `../README.md`
  - Project overview, current status snapshot, quick start
- `specifications/CORELX_SYNTAX_V1.md`
  - CoreLX v1 language syntax charter (decided 2026-06-12)
- `specifications/CORELX_CARTRIDGE_FORMAT.md`
  - CoreLX single-file cartridge format (draft)
- `../Games/NitroPackInDemo/CORELX_EXTRACTION.md`
  - CoreLX design decision record (M7) and M8 build order
- `specifications/COMPLETE_HARDWARE_SPECIFICATION_V2.1.md`
  - Current evidence-based hardware specification (base hardware)
- `specifications/APU_FM_OPM_EXTENSION_SPEC.md`
  - Current FM extension design + implementation status (YM2608-targeted FM path with backend fallback controls)
- `CORELX.md`
  - Reference for the **current shipping compiler** (the v1 charter above is where the language is going)
- `testing/README.md`
  - Current test command entrypoints and testing docs map

### The two end-user books (v1, in progress)

The project ships two distinct manuals for two audiences:

- `NITRO_CORE_DX_OWNERS_MANUAL.md` — **Console Owner's Manual** (player-facing):
  what the console is, the controller, running games. Clean Retro Code Ramen
  product voice.
- `CORELX_PROGRAMMING_GUIDE.md` — **Programming Guide** (programmer-facing): the
  full DevKit + CoreLX teaching, taught by Fletcher. Every demo program in it is
  compiled and run against the emulator by the test suite
  (`internal/corelx/manual_examples_test.go`, sources in `manual_examples/`).
  Style governed by `CORELX_MANUAL_STYLE_GUIDE.md`.

## Deferred Until CoreLX v1 Ships

- `../PROGRAMMING_MANUAL.md` and `guides/PROGRAMMING_GUIDE.md`
  - Both document the current pre-v1 compiler and will be rewritten against
    the finished v1 language. Until then they carry scope notes and remain
    usable for the shipping compiler.
- `../SYSTEM_MANUAL.md`
  - Under revision; verify claims against current hardware specs/tests.

## Documentation Organization

- `specifications/`
  - Language specs (CoreLX v1), hardware specs, pinouts, FPGA docs, FM extension spec
- `planning/`
  - Active product planning: `V1_CHARTER.md` (product V1 scope — distinct from
    the CoreLX *language* v1 charter), `V1_ACCEPTANCE.md`, `V1_RISKS.md`,
    `NEXT_STEPS_PLAN.md`, `FUTURE_FEATURES_PARKING_LOT.md`
- `testing/`
  - Test procedures and current workflows
- `guides/`
  - Setup/procedural guides (build, releases, debugging, EOD procedure)
- `archive/`
  - Superseded plans, historical reviews/audits, incident postmortems —
    retained for history, never current status

## Documentation Status Conventions

- `Current`: intended as source-of-truth / active reference
- `Under Revision`: useful but may contain stale assumptions; verify against current specs/tests
- `Historical Snapshot`: retained for context/history; do not use as current project status
- `Archive`: superseded content moved out of the active docs path

## Cleanup Notes (2026-06-12)

- Documentation pass aligned everything with the CoreLX v1 design decisions:
  resolved-issue postmortems and stale meta-cleanup docs were deleted (history
  lives in git); historical audits, the NitroLang design doc, the CoreLX data
  model plan, and completed planning checklists moved to `archive/`.
- Two "V1"s exist by design: the **product** V1 (`planning/V1_CHARTER.md` —
  SDK/emulator release scope) and the **CoreLX language** v1
  (`specifications/CORELX_SYNTAX_V1.md` — language freeze). Cross-references
  in both files distinguish them.
