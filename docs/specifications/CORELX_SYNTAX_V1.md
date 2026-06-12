# CoreLX v1 Syntax Charter

Status: **decided 2026-06-12** (project owner directive: optimize for
learnability; taste anchors are Lua, BASIC, and Go — simple is better).
This charter governs the M8 rebuild and freezes at v1 under the stability
contract (semantic changes forbidden after v1; evolution is additive).

The exemplar below uses every construct in the language. If a construct
isn't here, v1 doesn't have it.

```corelx
--! corelx 1.0
--! modules: anim, sfx

-- A small complete program. Comments are Lua-style.

const SPEED = 3.6                -- decimal literal => fixed (8.8)
const WORLD_MAX = 1023           -- integer literal => int (16-bit signed)

var scene: int = 0               -- global: compiler-placed in WRAM
var frames_seen: int = 0
var dma_buffer at 0x0500: u8[96] -- pinned global (hardware-sensitive)

struct Player:
    x: fixed
    y: fixed
    lives: int

function clamp(value: int, low: int, high: int) -> int
    if value < low
        return low
    if value > high
        return high
    return value

function update_player(p: Player)        -- structs pass by reference
    input.poll()
    if input.held(UP) and not input.held(DOWN)
        p.y = p.y - SPEED                -- fixed math: no manual shifts
    p.x = clamp(p.x, 0, WORLD_MAX)

function Start()
    player := Player()                   -- := declares locals
    player.lives = 3                     -- = assigns

    for i = 0 to 63                      -- BASIC counting loop, inclusive
        matrix_plane.set_tile(0, i, 0, 1, 0)

    while true
        wait_vblank()
        update_player(player)
        text.draw(8, 8, rgb(255, 255, 255), "HELLO NITRO")
        frames_seen = frames_seen + 1

-- ======== sprites ========  (banner comment written by the editor)

sprite Hero:
    size: 16
    palette_bank: 1
    palette: hex 0000 7fff 03ff
    data: hex
        00 00 11 11 ...
    anim walk_up:    1 2 3 4
    anim walk_right: mirror_h walk_left
```

## The decisions

### D1. Comments and directives
`--` comments (Lua). `--!` directives, legal only at the top of the file
before any code; unknown directives are compile errors.

### D2. Blocks by indentation
Indentation-delimited blocks (already implemented and proven). No `end`,
no braces — one less thing to forget, and the code's shape *is* its
structure. Tabs or consistent spaces; mixing is an error.

### D3. Three declaration forms, lifetime visible at the declaration
- `x := expr` — local variable (type inferred)
- `var name: type [= expr]` / `var name at 0xNNNN: type` — global (WRAM)
- `const NAME = expr` — compile-time constant, no storage

`=` assigns to something that already exists. Reading any declaration
tells you the lifetime without looking anywhere else.

### D4. Two numeric types to learn, two more for hardware
- `int` — 16-bit signed. The default. Integer literals are `int`;
  hex literals (`0x8010`) are `int`/`u16` as context requires.
- `fixed` — 8.8 fixed point. **Decimal literals are `fixed`**: writing
  `3.6` just works; no `fp()` wrapper, no manual shifts. Mixed
  `fixed`/`int` arithmetic without an explicit conversion
  (`int(f)`, `fixed(i)`) is a compile error.
- `u8`, `u16` — memory-layout types for struct fields, pinned buffers,
  and hardware-facing code. Taught later; most game code never needs them.

### D5. Words for logic, symbols for bits
`and`, `or`, `not`, `true`, `false` (Lua words — already in the lexer).
Bitwise stays symbolic (`&`, `|`, `^`, `<<`, `>>`) because register work
is symbol-shaped in every reference manual the user will read.

### D6. Conditionals
`if` / `else if` / `else`, no parentheses required around conditions.

### D7. Loops
- `while cond`
- `for i = 0 to 9` — BASIC counting loop, **inclusive** bounds, optional
  `step n` (negative steps count down). The loop variable is a fresh local.
- `break` and `continue` work in both.

### D8. Structs are reference types; there is no `&`
Numbers copy; structs are shared. `update_player(player)` lets the callee
modify the caller's struct — same rule as Lua tables. There is no
address-of operator, no pointer type, and no dereferencing.

### D9. Calls and namespaces
- Builtins and modules: `namespace.function(...)` — `bg.enable(0)`,
  `anim.play(hero_anim, Hero.walk_up)`. One calling convention everywhere.
- User functions: bare names — `clamp(x, 0, 9)`.
- Functions declare with `function name(param: type, ...) [-> type]`.

### D10. Data declarations are the section grammar
`sprite`, `background`, `music`, `instrument`, `sample` are top-level
declaration keywords — they ARE the data format. There is **no separate
section-marker syntax**: the parser doesn't need one, and the visual
"sections" the cartridge format promises are banner comments written and
maintained by the devkit editors. Convention: all data declarations live
below the code; editors enforce this when writing.

Asset names are plain identifiers in the same namespace as globals.
Dotted sub-names (`Hero.walk_up`, `ParkFloor.door`) reference anims and
regions.

### D11. Strings
Double-quoted string literals (lexer/parser support already exists),
usable with `text.draw` and game dialogue code. No string variables,
concatenation, or indexing in v1 — strings are labels, not a data type.
(Additive growth is possible later if real need appears.)

## Why this is the learnable set

A complete beginner must absorb exactly: comments, `:=`/`var`/`const`,
`int`/`fixed`, words-for-logic, `if`, `while`/`for-to`, `function`,
structs-are-shared, dotted calls, and "data declarations look like what
they are." Every one of those is either Lua, BASIC, or Go's most
copied idea. There are no pointers, no references, no manual fixed-point,
no asset prefixes, no section markers, and no second way to do anything.
