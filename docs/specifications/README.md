# Specifications (Current vs Historical)

This directory contains hardware specifications, pin definitions, FPGA documentation, and extension specs.

## Current / Preferred Specs

- `CORELX_SYNTAX_V1.md`
  - CoreLX v1 language syntax charter (decided 2026-06-12; governs the M8 rebuild and the v1 freeze)
- `CORELX_CARTRIDGE_FORMAT.md`
  - CoreLX single-file cartridge format draft (sections, text asset encoding, editor round-trip contract)
- `COMPLETE_HARDWARE_SPECIFICATION_V2.1.md`
  - Current evidence-based base hardware spec (CPU/PPU/APU/input/timing/registers)
- `PPU_TRANSFORM_CHANNEL_ARCHITECTURE.md`
  - Stage 1 architecture baseline for the long-term PPU transform/raster model (multi-channel affine planes, raster command tables, emulator/FPGA target contract)
- `PPU_MATRIX_PLANE_MEMORY_SPEC.md`
  - Matrix-plane source-size and dedicated-memory baseline spec (minimum 1024x1024 per plane, outside behavior, emulator-first implementation direction)
- `APU_FM_OPM_EXTENSION_SPEC.md`
  - Current FM extension runtime design + implementation status (YM2608-targeted host interface + backend selection/fallback model)
- `CARTRIDGE_PIN_SPECIFICATION.md`
  - Cartridge connector pinout/spec
- `CONTROLLER_PIN_SPECIFICATION.md`
  - Controller connector pinout/spec

## Active Supporting Specs / FPGA Docs

- `FPGA_IMPLEMENTATION_SPECIFICATION.md`
- `FPGA_ARCHITECTURE_RECOMMENDATION.md`
- `FPGA_READINESS_ASSESSMENT.md`
- `FPGA_READINESS_COMPARISON.md`
- `SPEC_AUDIT_DISCREPANCIES.md`
- FPGA docs in this section describe the **target FPGA architecture/specification direction**.
- They are not a guarantee that the in-tree RTL is already at full emulator parity.
- Current RTL-vs-emulator implementation status should be cross-checked against:
  - `../HARDWARE_FEATURES_STATUS.md`
  - current FPGA source under `FPGA/nitro_core_dx_fpga/src/`
- Implementation status snapshots:
  - `../HARDWARE_FEATURES_STATUS.md`
  - `SPEC_AUDIT_DISCREPANCIES.md`

## Historical / Superseded Specs (Keep for Context)

- `HARDWARE_SPECIFICATION.md` (older v1.0; contains stale APU/FM details)
- `COMPLETE_HARDWARE_SPECIFICATION.md` (older v2.0; superseded by v2.1)

## Notes

- If hardware/audio details conflict, prefer:
  1. `COMPLETE_HARDWARE_SPECIFICATION_V2.1.md`
  2. `APU_FM_OPM_EXTENSION_SPEC.md` (for FM-specific behavior/status)
  3. current source code/tests
  4. `docs/planning/V1_CHARTER.md` / `V1_ACCEPTANCE.md` for release-target direction (YM2608 is the V1 target)
