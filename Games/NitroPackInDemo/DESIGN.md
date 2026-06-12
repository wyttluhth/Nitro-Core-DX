# NitroPackInDemo Design

> **Status (2026-06-12):** Milestones M1-M7 are complete — the ROM demo loop
> (title, overworld, interior, dialogue, credits) is finished and tested, and
> the M7 design extraction is recorded in
> [CORELX_EXTRACTION.md](CORELX_EXTRACTION.md), which also carries the CoreLX
> v1 design decisions. Active work is Milestone 8: the CoreLX rebuild. This
> document is the demo's original design plan, kept as the reference for the
> rebuild's acceptance behavior.

## Purpose

`NitroPackInDemo` is the ROM-first global showcase/demo project for Nitro-Core-DX.

Its job is to prove that the engine can support a pseudo-3D RPG/adventure-style
game loop and then become the reference application used to shape CoreLX.

The project is intentionally staged in this order:

1. build and validate the complete experience as a low-level ROM first
2. use the working ROM to inform CoreLX language design and runtime APIs
3. rebuild the same demo in CoreLX
4. compare ROM-first and CoreLX outputs to verify that the language, compiler,
   and runtime abstractions behave the way we want

This demo is also intended to become the future pack-in/sample experience for
the Nitro-Core-DX Dev Kit.

## Project Identity

- Project name: `NitroPackInDemo`
- Primary implementation strategy: `ROM-first`
- Later follow-up strategy: `CoreLX parity rebuild`
- Genre target: pseudo-3D adventure / RPG framework demo
- Visual baseline: `matrix_floor_billboard_generic.rom`
- Audio baseline: start with overworld YM2608 playback from
  `roms/pong_ym2608_demo.rom`

## Experience Goals

The player should feel like they are stepping into a small pseudo-3D world that
shows off what Nitro-Core-DX can do today and what CoreLX should be able to
express ergonomically later.

The demo should communicate:

- pseudo-3D overworld exploration
- visible third-person player character
- billboard buildings and props
- scene transitions
- interior 3D room rendering
- dialogue interaction
- credits flow
- YM2608-backed music playback
- pause handling
- jump and player animation

This is not meant to be a full game. It is meant to be a polished, playable,
feature-proving framework that could serve as the skeleton for a future adventure
or Mode-7-style RPG.

## Player Flow

The intended start-to-finish flow is:

1. Title screen
2. `Press Start`
3. On start:
   - transition out of title
   - begin overworld YM2608 song
   - enter the overworld scene
4. Overworld:
   - player walks around a small pseudo-3D area
   - player can approach buildings and props
   - player can approach a designated building door
5. At the building door:
   - player presses `A` / Button 1
   - scene changes to an interior showcase room
6. Interior room:
   - player can walk around the room in pseudo-3D
   - room shows floor, ceiling, walls, logo wall, and NPC
7. At the NPC:
   - player presses `A` / Button 1
   - dialogue says `Thanks for trying the demo`
   - transition to credits
8. Credits page
9. Optionally return to title or remain at credits end state

## Input Model

The current input design target is:

- `Start`
  - title: begin demo
  - gameplay: pause/unpause
- `Up`
  - move forward
- `Down`
  - move backward
- `Left`
  - rotate camera / player heading left
- `Right`
  - rotate camera / player heading right
- `A` / Button 1
  - interact / talk / enter building / advance prompts
- `B` / Button 2
  - jump

Pause behavior target:

- switch to a black screen
- display `PAUSE` centered
- old SNES / Genesis style presentation
- gameplay logic should freeze while paused

## Camera and Movement Model

The movement target is not tile-step movement.

The demo should follow the feel of `matrix_floor_billboard_generic.rom`:

- free 360-degree heading
- player rotates via left/right
- player moves relative to current heading
- visible player sprite remains in third-person view
- player sprite is anchored near the middle of the screen

This is effectively:

- pseudo-3D third-person adventure movement
- not top-down 2D
- not first-person
- not grid-based

The player should always remain visible in both the overworld and the interior.

## Scene List

The canonical scene list is:

- `SceneTitle`
- `SceneOverworld`
- `SceneInteriorRoom`
- `SceneDialogue`
- `SceneCredits`
- `ScenePauseOverlay`

The exact implementation can use an enum, table, or dispatch layer, but scene
state should be explicit and centralized.

## Scene Design

### Title Scene

Purpose:

- establish branding
- communicate that this is a polished demo
- gate the start of the overworld music and main runtime

Requirements:

- Nitro-Core-DX title art or placeholder title layout
- `Press Start`
- possibly subtle background animation later
- no gameplay movement here

First-pass acceptable state:

- static title screen
- text prompt
- press start transition

### Overworld Scene

Purpose:

- serve as the main gameplay and traversal showcase
- prove pseudo-3D movement, billboard rendering, player sprite animation,
  object placement, and scene transitions

Layout target:

- one small demo area
- one edge of the world contains a row of building fronts
- at least one building is enterable
- additional buildings may be decorative or future-interactive
- include tree sprites and world props managed as billboarded world objects

Rendering target:

- floor plane uses current matrix floor path
- buildings and props use billboard-style matrix transforms
- trees and similar props should behave like kart-style billboard objects that
  always face camera appropriately

Gameplay target:

- movement
- turning
- jumping
- collision against building line / boundaries
- interact trigger at designated door

Music target:

- overworld YM2608 music starts on leaving title
- overworld music loops only while on overworld

### Interior Showcase Room

Purpose:

- show off enclosed pseudo-3D space
- prove the engine can support a room made from matrix-driven surfaces plus
  characters and interaction

Room target:

- one showcase room only
- floor
- ceiling
- four walls
- back wall contains Nitro-Core-DX logo
- NPC in room

Rendering target:

- floor and ceiling should use the matrix path
- ceiling should follow the intended `ceiling matrix path` idea similar in
  spirit to the SNES split approach the project has discussed
- walls should use matrix-driven or equivalent transform primitives that read as
  a coherent 3D room

Gameplay target:

- player can walk around freely
- player remains visible in third person
- room boundaries block movement
- NPC can be approached and interacted with

Music target:

- separate interior track is desired later
- first pass may use silence or temporary reuse until interior music is chosen

### Dialogue Scene

Purpose:

- prove reusable message box system
- create a bridge from NPC interaction to credits

Target interaction:

- player approaches NPC
- presses `A`
- dialogue box appears
- message thanks the player for trying the demo
- transition to credits

This should be implemented as a reusable system, not one hardcoded case.

### Credits Scene

Purpose:

- close the demo cleanly
- provide a future reusable credits flow for CoreLX

Target:

- static or scrolling credits presentation
- should eventually map cleanly to a high-level CoreLX credits helper

First-pass acceptable:

- static credits page with clean layout

### Pause Overlay

Purpose:

- prove gameplay suspension and overlay presentation

Target:

- black screen
- centered `PAUSE`
- triggered by `Start`
- resumes back to current scene

This should be implemented as a reusable overlay/state behavior.

## Core Systems Required

The ROM must include reusable systems rather than one-off scene hacks.

### 1. Scene Manager

Must support:

- current scene state
- enter/exit hooks
- scene-local update/render setup
- transition calls
- pause overlay routing

### 2. Player Controller

Must support:

- world position
- heading
- movement state
- jump state
- animation state
- collision response
- scene-safe spawn positioning

### 3. Camera Model

Must support:

- heading-relative movement
- player-centered third-person presentation
- consistent overworld and interior behavior
- stable sprite anchoring

### 4. Sprite System

Must support:

- player sprite
- NPC sprite
- possible tree/prop sprite paths if not matrix-billboarded
- animation states
- facing/turn state mapping
- jump visual state

### 5. Billboard World Object System

Must support:

- building fronts
- trees
- props
- NPC or decorative characters if needed
- camera-facing logic
- depth/order rules
- interaction tagging

### 6. Collision and Trigger System

Must support:

- world bounds
- blocking geometry
- interaction volumes
- door trigger
- NPC trigger range

### 7. Dialogue System

Must support:

- text box open/close
- line or page progression
- future extensibility for portraits or multiple pages
- reusable data-driven calls

### 8. Credits System

Must support:

- credits start call
- render/update behavior
- end-of-demo handling

### 9. Music and Audio Control

Must support:

- start overworld song on leaving title
- keep it looping only in overworld
- stop or switch when leaving overworld
- future room music slot

### 10. Pause System

Must support:

- suspend gameplay update
- overlay render state
- resume cleanly

## Rendering / Engine Requirements

This ROM is also an engine validation target.

The following rendering capabilities are either required immediately or must be
added during implementation:

- matrix floor rendering for overworld traversal
- billboard object rendering for buildings and props
- third-person player sprite composited over pseudo-3D world
- enclosed room support with:
  - floor
  - ceiling
  - four walls
- stable ordering between:
  - floor
  - ceiling
  - walls
  - player sprite
  - NPC sprite
  - billboard props
- scene transition reliability

The room implementation is expected to expose engine gaps. That is intentional.
Those gaps should be fixed in the engine rather than worked around in fragile
demo-specific code whenever practical.

## Asset Plan

The asset strategy is explicitly:

- placeholder first
- polish later

Initial placeholder asset list:

- title screen placeholder art / logo layout
- player sprite:
  - idle
  - walk
  - jump
- NPC sprite
- building front placeholder texture(s)
- tree placeholder sprite / billboard texture(s)
- wall texture placeholder
- ceiling texture placeholder
- floor texture placeholder
- back-wall Nitro-Core-DX logo asset
- credits screen background/layout asset if needed

Asset polish phase can happen after full start-to-finish functionality exists.

## Proposed ROM Architecture

The ROM should be treated like a real game framework, not a one-off graphics
experiment.

Recommended runtime structure:

- bootstrap / init
- global game state
- per-scene setup/update functions
- rendering setup helpers per scene
- object table(s)
- trigger table(s)
- asset references / upload helpers
- music control hooks

Recommended logical subsystems:

- `GameState`
- `SceneState`
- `PlayerState`
- `CameraState`
- `ObjectState`
- `DialogueState`
- `AudioState`
- `CreditsState`

The exact source layout can evolve, but the systems should remain distinct.

## Development Strategy

The correct build order is vertical-slice-first, not title-first.

The first real objective is:

- a working overworld slice with player movement, player sprite, billboards,
  collision, and a functional door transition

Everything else should wrap around that foundation.

## Milestones

> **Implementation status (June 2026):** Milestones 0-5 are implemented in the
> ROM-first builder (`build_rom.go`) and covered by the headless scene-flow
> tests. The interior room currently ships as a simplified enclosure — a
> matrix-plane floor with a painted wall band and void surround rather than
> separate ceiling/wall planes or a back-wall logo; those remain Milestone 6
> polish items. Milestone 6 (polish) and Milestones 7-8 (CoreLX extraction and
> rebuild) are the active next steps.

### Milestone 0: Design Lock

Deliverables:

- this design doc
- final confirmation of scene flow, controls, and project scope

Exit criteria:

- project direction is stable enough to start ROM implementation

### Milestone 1: Shared Runtime Framework

Deliverables:

- scene manager
- global state layout
- input abstraction
- player/camera state
- trigger/collision scaffolding
- dialogue box scaffold
- pause scaffold
- music control scaffold

Exit criteria:

- ROM has a clean framework for scene-driven development

### Milestone 2: Overworld Vertical Slice

Deliverables:

- small overworld area
- floor plane
- row of building fronts
- tree/prop billboard objects
- visible player sprite
- turning, movement, jump
- collision
- working interactable building door

Exit criteria:

- player can move around and enter a building trigger

### Milestone 3: Presentation Layer

Deliverables:

- title screen
- press start flow
- pause screen
- overworld music integration

Exit criteria:

- title -> start -> overworld -> pause all function cleanly

### Milestone 4: Interior Showcase Room

Deliverables:

- enterable interior scene
- floor
- ceiling
- four walls
- back-wall logo
- NPC placement
- room collision

Exit criteria:

- player can walk inside a stable enclosed pseudo-3D room

### Milestone 5: Interaction Completion

Deliverables:

- NPC interaction
- thank-you dialogue
- credits transition
- credits page / sequence

Exit criteria:

- full end-to-end playable loop from title to credits exists

### Milestone 6: Polish Pass

Deliverables:

- placeholder art cleanup
- animation refinement
- transition polish
- second track support if desired
- readability and presentation cleanup

Exit criteria:

- demo is presentable as a pack-in showcase

### Milestone 7: CoreLX Design Extraction

Deliverables:

- document all ROM features that should become CoreLX systems or helpers
- identify syntax and API pain points from ROM-first implementation

Exit criteria:

- CoreLX feature requirements are explicit and tied to a working reference demo

### Milestone 8: CoreLX Rebuild

Deliverables:

- rebuild the demo in CoreLX
- compare outputs/behavior against ROM-first reference

Exit criteria:

- CoreLX version reproduces the intended experience closely enough to validate
  language and compiler design

## CoreLX Features This Demo Must Drive

This project is a CoreLX design vehicle. The ROM should explicitly inform APIs
for:

- scenes and scene transitions
- player movement helpers
- sprite management
- sprite animation
- music playback control
- collision helpers
- interaction triggers
- dialogue box system
- credits system
- matrix transform helpers
- matrix floor helpers
- ceiling helpers
- billboard object helpers
- asset loading/import

Save/load is explicitly not required for this demo.

## CoreLX Design Principle

The lesson we want from this demo is not:

- `CoreLX should just expose raw register writes`

The lesson we want is:

- common game tasks should have ergonomic high-level helpers
- advanced rendering/audio work should still allow low-level escape hatches

This project should push CoreLX toward being a robust game language rather than
only a thin hardware scripting layer.

## First Acceptable "Done Enough to Demo" State

The first meaningful completion target is:

- full start-to-finish playable flow
- placeholder art
- simple but clean transitions
- working overworld
- working room
- working NPC interaction
- working credits

Polish can follow after that.

## Risks

Primary risks:

- interior room composition may expose missing engine features
- ceiling implementation may need new matrix/ordering behavior
- third-person sprite + billboard composition may need careful priority tuning
- pseudo-3D collision may become messy if not kept simple early
- trying to polish assets too early could slow down system completion

Mitigation strategy:

- prioritize system completeness over polish
- keep overworld small
- keep one room only
- solve engine problems at engine level when possible
- use the ROM as a disciplined reference target

## Current Recommended Immediate Next Step

The next implementation step should be:

- create the ROM project scaffold for `NitroPackInDemo`
- start with the shared runtime framework
- then build the overworld vertical slice first

The immediate engineering order should be:

1. scene/state framework
2. player/camera state
3. overworld floor + movement
4. player sprite rendering and animation
5. billboard world objects
6. collision + door trigger
7. scene transition into placeholder interior

Only after that should title, pause, full room polish, and credits be layered in.

## Canonical Project Rule

If future context windows lose track of intent, return to this rule:

`NitroPackInDemo` is a ROM-first pseudo-3D adventure showcase whose purpose is
to prove engine capability and then directly shape CoreLX into a practical game
development language.
