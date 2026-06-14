# The Nitro-Core-DX Programming Guide — DRAFT

### Making games with CoreLX and the DevKit

**A Retro Code Ramen publication for Nitro-Core-DX.**
Written by AJ / Retro Code Ramen. Your guide through the machine is Fletcher,
who is not the author, does not want the paperwork that comes with being the
author, and would like that noted up front.

> **This is the programmer's book.** If you just want to play games on your
> Nitro-Core-DX, you want the **Console Owner's Manual**
> (`NITRO_CORE_DX_OWNERS_MANUAL.md`) instead. This guide is for building them:
> the CoreLX language, the DevKit, and how to make the machine do what you want.

> Status: living draft. Sections appear here only after the feature they
> describe is implemented and verified running on the Nitro-Core-DX core — so
> nothing in this guide is a promise. It already works, or it isn't in here
> yet. Every demo program in this book is compiled and run against the real
> emulator core by the test suite; if one ever stops working, the build breaks.
> The normative spec is `specifications/CORELX_SYNTAX_V1.md`; this book is where
> you actually learn the thing. Voice and structure follow
> `CORELX_MANUAL_STYLE_GUIDE.md`.

---

## A short word before Fletcher takes over

CoreLX is the language you use to make the Nitro-Core-DX do things. Not a
general-purpose language you later bend toward a console — a language shaped
around *this* machine: its 16-bit registers, its WRAM, its very specific
opinions about numbers. You will learn it by building real programs and then
breaking them on purpose, because that is the fastest way to understand any
machine that has ever existed.

Fletcher will handle the rest. He has been here longer than the documentation.

---

## Chapter 1 — Two Kinds of Numbers, and Why One of Them Lies to You

You write `3`. You write `3.6`. Looks like the same kind of thing, right? Two
numbers, one of them has a dot. On most machines you'd be correct and we could
all go home.

Not here.

> **Fletcher:** Sit down. The first thing that bites everybody on this board is
> numbers, and it bites them precisely *because* they assume numbers are
> boring. On the DX, the moment you put a dot in a number, you've changed what
> kind of number it is. Not how it prints. What it *is*. I have watched grown
> engineers lose an afternoon to this. We're going to lose ninety seconds
> instead.

CoreLX has two numeric types you'll touch constantly:

- **`int`** — a 16-bit signed whole number. Range −32768 to 32767. Every plain
  integer you write is an `int`: `3`, `1023`, `0x8010`.
- **`fixed`** — a fractional number stored in **8.8 fixed-point**. Eight bits
  for the whole part, eight for the fraction. And here is the part that trips
  people: **every decimal literal is a `fixed`.** Write `3.6`, `0.5`, `1.75`
  and you have written `fixed` values. The machine made that decision for you
  the instant you typed the dot.

`fixed` covers roughly −128.0 to +127.996, in steps of 1/256. That's your
fraction resolution: 1/256, about 0.004. Smaller than that and the DX shrugs.

Here is a small program that moves a number around. It compiles and runs:

```corelx
const SPEED = 3.6            -- has a dot, therefore fixed
const WORLD_MAX = 1023       -- no dot, therefore int

var x: fixed = 64.0
var lives: int = 3

function Start()
    x = x + SPEED            -- fixed plus fixed: the machine is content
    x = x * 0.5              -- fixed times fixed: also fine, full precision
    while true
        wait_vblank()
```

Nothing surprising yet. Now let's make it angry.

### Breaking it on purpose

You've got a speed in `fixed` and a counter in `int`. Naturally you try to
multiply them, because that's a completely reasonable thing a person would do:

```corelx
var speed: fixed = 1.5
var count: int = 3
out = speed * count          -- this does NOT compile
```

The compiler stops you cold:

```
cannot mix fixed and int in '*' — convert explicitly with int(x) or fixed(x)
```

> **Fletcher:** Good. That error is the machine doing you a favor, even though
> it doesn't feel like one. `fixed` and `int` store their bits completely
> differently — `1.5` in `fixed` is the bit pattern `0x0180`, not the number
> one-and-a-half sitting in a register being polite. If the compiler let you
> multiply them as if they were the same thing, you wouldn't get an error.
> You'd get a *wrong answer*, silently, three weeks from now, in a build you've
> already shown people. I would rather yell at you today.

### What went wrong, and how to say what you meant

You have to convert, out loud, in the direction you actually want:

```corelx
out = speed * fixed(count)   -- 1.5 * 3.0 = 4.5  (count promoted to fixed)
whole = int(speed) * count   -- 1 * 3 = 3        (speed chopped down to int)
```

`fixed(i)` turns an int into a fixed: `3` becomes `3.0`. `int(f)` throws away
the fraction and keeps the whole part: `4.5` becomes `4`. That's the entire
conversion story. Two functions, both say exactly what they do.

> **Field Notes:** The DX has no floating-point unit. None. It was never going
> to. Floating-point hardware is expensive in gates and this is a console that
> wants those gates for making things move on screen. `fixed` is the old,
> honest trick: store the fraction as a plain integer count of 1/256ths and
> agree, as a civilization, where the decimal point lives. Every racing game's
> sense of speed on hardware like this was built on exactly this idea.

> **Raccoon Engineering:** Need to divide a `fixed` by two? You *can't* divide
> `fixed` by `fixed` yet on this machine — but you don't need to. Multiply by
> `0.5` instead. Want a third? Multiply by `0.333`. Reciprocals are your
> friend, they're faster anyway, and the multiply path is fully supported and
> keeps full precision. Division is the expensive door; most of the time you
> can just walk around it.

> **Fletcher's Warning Label:** Two real limits, both of which the compiler
> will tell you about so you don't have to memorize them: (1) `fixed / fixed`
> isn't available — use the reciprocal trick above. (2) Integer division is
> **unsigned** in v1. If you're dividing negative `int`s and getting numbers
> from the upside-down, that's why. Work in positives and track the sign
> yourself until that changes.

### Try This Before You Panic

Make a `fixed` variable, set it to `0.1`, and add it to itself ten times.
Print the result or peek at it in the debugger. It will **not** be exactly
`1.0`, and that is not a bug — `0.1` isn't perfectly representable in 1/256
steps, so a tiny error rides along each time. This is the single most important
thing to feel in your hands early: `fixed` is precise, but it is not *infinite*.
Knowing where it rounds is the difference between a smooth game and a jittery
one.

---

## Chapter 2 — Constants: Naming Things So You Stop Lying to Yourself

Here's a number from a real program: `1023`. What is it? The right edge of the
world? A bitmask? The price of something? You wrote it last week and now you're
staring at it like it owes you money.

> **Fletcher:** Magic numbers are how code rots. Not dramatically — quietly. One
> day `1023` means the world edge, and six months later you change the world
> size, miss one of the eleven places you typed `1023`, and now your player can
> walk through a wall on the east side of the map only. I have debugged that
> exact bug. It took an hour. It should have taken zero, because the number
> should have had a *name*.

A `const` gives a value a name that's computed once, at compile time. It costs
you nothing at runtime — no memory, no slowdown. Every place you use it, the
value gets baked straight into the program.

```corelx
const BASE = 100
const DOUBLE = BASE * 2          -- constants can build on earlier ones
const FLAGS = 0x10 | 0x02        -- full integer math and bitwise ops
const HALF_SPEED = SPEED / 2.0   -- fixed constants work too: + - * /
```

Use them for everything that's a *fact* about your game: world bounds, zone
edges, speeds, hardware register values, the number of lives you start with.
If you're typing the same number twice, it wants to be a `const`.

### Breaking it on purpose

Try to change one:

```corelx
const LIMIT = 10
function Start()
    LIMIT = 5            -- nope
```

```
cannot assign to constant LIMIT
```

> **Fletcher:** Right, and that's the *point* of the word "constant," so I'm not
> going to pretend that's a surprise. But here's the genuinely useful part:
> because constants are resolved at compile time, the machine does the math
> *before the game ever runs*. `DOUBLE = BASE * 2` isn't a multiply on the
> console — by the time your cartridge boots, `DOUBLE` is just `200`, sitting
> there, already done. Free arithmetic. Use it shamelessly.

> **Field Notes:** Remember Chapter 1 — `HALF_SPEED = SPEED / 2.0` works even
> though you can't divide `fixed` by `fixed` at *runtime*. That's because a
> constant divide happens in the compiler, on the workbench, not on the
> machine. The compiler is allowed tools the console isn't. It computes the
> answer once and hands the console a finished number.

### Try This Before You Panic

Take a program you've already got and hunt for every raw number that appears
more than once. Give each one a `const` with a name that says what it *is*, not
what it equals. `const WORLD_MAX = 1023`, not `const TEN_TWENTY_THREE = 1023`.
Future-you is the one you're writing these names for.

---

## Chapter 3 — Globals, and the Map of Where Everything Lives

You've got a score. The title screen needs it, the gameplay needs it, the
game-over screen needs it. It has to live somewhere every part of your program
can reach. That somewhere is a **global**, and it lives in WRAM — the
machine's working memory.

```corelx
var scene: int = 0           -- the compiler picks the address
var score: int               -- no initializer means it starts at 0
var energy: u8 = 255         -- u8: one byte, holds 0 to 255
var player_x: fixed = 64.0
```

A `var` at the top level — outside any function — is global. It exists for the
whole life of the game, and every function can see it. Initializers run once,
at power-on, before `Start()` does anything.

> **Fletcher:** On a lot of machines you'd be picking memory addresses by hand
> here, like an animal, writing `score` at `0x2100` and `lives` at `0x2102` and
> praying you never overlap two things. I did that for years. It is exactly as
> fun as it sounds. CoreLX does it for you now, and — this is the part I
> actually like — it writes down *where it put everything*.

### The map

The compiler allocates globals automatically, starting at WRAM address
`0x2100`. You never pick addresses, and you can never accidentally land two
variables on the same spot. And every time you build, it drops a **memory map**
file next to your ROM — `yourgame.rom.memmap` — listing every global, its
address, and its size. When you're knee-deep in the debugger at 2 a.m., that
file tells you exactly where `score` actually lives.

Three regions of WRAM are worth knowing:

| Region | Whose it is |
|---|---|
| `0x2000`–`0x20FF` | the compiler's own runtime scratch — **hands off** |
| `0x2100` upward | your globals, placed automatically |
| `0x7000`–`0x7FFF` | **yours, forever** — the compiler never touches it |

That last region matters. If you're doing something raw and clever with
`mem.*` pokes and you want memory the compiler will *never* step on, `0x7000`
to `0x7FFF` is your sandbox. Guaranteed.

### When you actually do need a specific address

Sometimes the hardware cares where something lives — a buffer you're going to
stream to a register, a table the DMA reads. You can pin a global to an exact
address:

```corelx
var dma_buffer at 0x7200: u8[96]
```

### Breaking it on purpose

Pin something somewhere stupid:

```corelx
var oops at 0x2080: int        -- inside the compiler's runtime block
```

```
global oops pinned at 0x2080 overlaps the reserved runtime block (0x2000-0x20FF)
```

> **Fletcher:** And it'll catch you the same way if you pin two things on top of
> each other, or drop a pin into the I/O registers up at `0x8000`. Pinning is a
> sharp tool. The compiler checks the blade before you grab it. Notice I pinned
> `dma_buffer` at `0x7200` — up in *your* region, not down in the auto-allocated
> pile where it'd collide with the variables the compiler is placing. Pin into
> your own sandbox, not someone else's workbench.

> **Tape Jam:** Variable not holding what you expect, and the value looks like
> garbage that *almost* makes sense? Before you blame your logic, open the
> `.memmap` file and confirm the address you're poking in the debugger is
> actually the variable you think it is. Half of all "impossible" memory bugs
> are just looking at the wrong address with great confidence.

### Try This Before You Panic

Build any program with two or three globals in it, then open the `.memmap`
file the compiler wrote. Read it. See `score` at `0x2100`, see the next one
sitting right after it. Get comfortable with that file now, while the stakes
are low — it becomes your best friend the first time something goes wrong in
memory, which it will.

---

## Chapter 4 — Arrays: When You Need a Row of the Same Thing

One score is a `var`. But sixty-four heading angles for a turning camera, or a
field of stars, or a row of tile values — those want to live together, in
order, reachable by number. That's an **array**.

```corelx
const N = 8
var table: int[8]
var palette_rows: u8[4]

function Start()
    i := 0
    while i < N
        table[i] = i * 10        -- any expression for the index
        i = i + 1
    total := table[3] + table[7] -- read them back by number
```

Arrays start zeroed — every slot is `0` until you put something there.
`int[n]` and `fixed[n]` use two bytes per slot; `u8[n]` uses one. They live in
WRAM right alongside your other globals, and they show up in the memory map.

### Breaking it on purpose

Reach past the end:

```corelx
var table: int[4]
function Start()
    table[4] = 1            -- there is no slot 4; valid slots are 0,1,2,3
```

```
index 4 out of bounds for table[4]
```

> **Fletcher:** Caught at *compile time*, before the cartridge ever boots,
> because that index was a constant the compiler could check. That's the good
> case. Now, the honest part you need to hear: if your index is something the
> machine only figures out *while running* — a variable, a result of math — the
> DX does **not** check it for you. It can't afford to. This is a 16-bit
> console with frames to render; it is not going to spend cycles babysitting
> every array access. Write past the end with a runtime index and you'll
> happily stomp on whatever WRAM sits after your array, and the machine will
> let you, whistling.

> **Fletcher's Warning Label:** A constant index, like `table[4]`, gets checked
> when you build. A computed index, like `table[i]`, does **not** get checked
> when it runs. That's not laziness, it's the deal you make for speed on real
> hardware. Keep your loop bounds honest — `while i < N`, not `while i <= N` —
> and you'll never feel the missing seatbelt. Get the bound wrong and you'll
> feel it as the strangest bug of your week.

> **Raccoon Engineering:** A pre-computed table beats math every time on this
> machine. If your game keeps calculating the same handful of values —
> sines for a spin, speeds for each heading — compute them once into a `const`-
> sized array at startup and just *look them up* after that. The DX reads
> memory faster than it grinds arithmetic. Trade a little WRAM for a lot of
> frame time. That's the whole trick behind half the smooth-looking effects on
> hardware like this.

### Try This Before You Panic

Make an `int[8]`, fill it in a loop with `table[i] = i * i`, and read the
values back in the debugger using the address from your `.memmap` file. You'll
see `0, 1, 4, 9, 16, 25, 36, 49` laid out in WRAM, two bytes each. Now
deliberately change your loop to `while i <= 8` and watch it write one slot too
far. Find what it landed on in the memory map. That's the bug you're learning
to never ship.

---

## A note on local variables (you've been using them)

Inside a function, `:=` makes a local and figures out the type from what you
give it:

```corelx
x := 5          -- int (no dot)
speed := 2.5    -- fixed (dot)
x = x + 1       -- plain = changes something that already exists
```

Three ways to make a name, and each one tells you that name's whole life at a
glance: `:=` is local to its function, `var` is a global in WRAM, `const` is a
compile-time fact with no storage at all. You'll never have to wonder how long
a name lives — you can see it in how it was born.

---

## Chapter 5 — Loops That Count: `for i = 0 to N`

You want to do something eight times. Fill eight table slots, draw eight stars,
check eight collision boxes. You *could* write it out eight times like you're
being paid by the line. You could also set up a `while` loop with a counter and
a manual increment and an off-by-one bug waiting to happen. Or you could just
say what you mean.

```corelx
for i = 0 to 7
    table[i] = i * 10
```

That runs with `i` equal to 0, 1, 2, 3, 4, 5, 6, 7. **Eight times.** The bounds
are *inclusive* — `0 to 7` means zero through seven, both ends, exactly like you
read it out loud.

> **Fletcher:** I want to stop you on that word "inclusive" because it is the
> single most common place people miscount. `for i = 0 to 7` runs eight times,
> not seven. If you've come from machines where the loop limit means "stop
> *before* this," unlearn that here. On the DX, `to 7` includes 7. Say the
> range out loud — "zero to seven" — and count on your fingers if you have to. I
> still do.

Need to count down, or skip? Add a `step`:

```corelx
for i = 10 to 0 step -2
    -- i is 10, 8, 6, 4, 2, 0  (six times)
```

### Breaking it on purpose

The `step` has to be a number the compiler knows when it builds — a constant,
not something computed while the game runs. Try to make it a variable:

```corelx
for i = 0 to 10 step my_var      -- nope
```

```
for loop 'step' must be a constant
```

> **Fletcher:** That's deliberate, and here's the why: the compiler needs to
> know which *direction* you're counting so it knows when to stop. Counting up,
> it stops when you pass the top. Counting down, it stops when you pass the
> bottom. If your step could secretly be positive *or* negative depending on
> some variable, the compiler can't pick the right finish line, and a loop with
> the wrong finish line either stops too early or runs until the heat death of
> your cartridge. So: `step` is a constant. Pick a direction at write-time.

> **Try This Before You Panic:** Loop `for i = 0 to 5` and in the body draw the
> counter on its own line: `text.draw_int(80, 40 + i * 12, 255, 255, 255, i)`.
> The `40 + i * 12` pushes each number twelve pixels lower than the last, so you
> get a column 0,1,2,3,4,5. Count the lines — there are six, not five. Now flip
> it to `for i = 5 to 0 step -1` and watch the column come out upside down.
> Feeling the inclusive bounds with your own eyes once beats me telling you ten
> times. (`text.draw_int` is in Chapter 6 — it draws a number instead of a
> string.)

---

## Chapter 6 — Putting Words on the Screen

Your game runs. Brilliant. But it's a black void, and the only person who knows
anything is happening is you, squinting at a debugger. Time to put something on
the glass. Start with text, because text is how you say `SCORE`, `GAME OVER`,
and `PRESS START` — the words every game needs.

```corelx
text.draw(40, 80, 255, 255, 255, "HELLO NITRO")
```

Six arguments: X, Y, then **three separate color numbers** — red, green, blue,
each 0 to 255 — and finally the string. So `255, 255, 255` is white, `255, 0, 0`
is red, and so on. The text appears at pixel (40, 80) and marches to the right,
eight pixels per character.

> **Fletcher:** I know what you're about to ask. "Why three color numbers?
> Every other system lets me pass one color." Because the DX's text port has
> three separate eight-bit channels — red, green, blue — sitting at three
> separate hardware addresses, and a single number on this machine is sixteen
> bits. Sixteen bits cannot hold three eight-bit channels. The math doesn't
> fit. So instead of lying to you with a fake "color" that secretly loses
> information, CoreLX hands the port exactly what it wants: R, G, B, each on its
> own. It's one more number to type and zero surprises later. I'll take that
> trade every day.

> **Field Notes:** When you write a string, the characters stream out to a
> single hardware register one at a time, and the port quietly advances the
> cursor eight pixels after each one. That's why your text flows left to right
> without you tracking position — the *port* is keeping the cursor, not you.
> It's the same trick the old machines used: a chip doing the boring
> bookkeeping so the programmer didn't have to.

### Breaking it on purpose

Strings are special on the DX. They're **labels**, not a data type you can
store and shuffle around. Try to put one in a variable:

```corelx
var name: int = 0
name = "PLAYER"          -- the compiler stops you
```

```
strings can only be used directly as a text.draw argument in v1
```

> **Fletcher:** Right, and before you grumble — this is a 16-bit console, not a
> word processor. v1 strings exist to *label things on screen*: scores, menus,
> the word "PAUSED." They are not a place to store the player's name and do
> clever text manipulation. Keeping them simple keeps them fast and keeps the
> machine honest. For now: a string goes straight into `text.draw`, full stop.

### Numbers on the screen

A string is a fixed label. But a *score* changes — it's a number that lives in a
variable and goes up. For that, there's a second function:

```corelx
var score: int = 0

function Start()
    score = 1230
    while true
        wait_vblank()
        text.draw(120, 80, 255, 255, 255, "SCORE")
        text.draw_int(140, 100, 255, 255, 0, score)
```

`text.draw_int` takes the same first five arguments as `text.draw` — X, Y, and
the three color channels — but the last argument is a **number**, not a string.
It prints it as digits: `1230` shows up as `1230`. Negatives get a minus sign,
and leading zeros are dropped, so `42` prints as `42`, not `00042`.

> **Fletcher:** This is the one you'll reach for constantly — score, lives,
> timer, the player's X position while you're debugging movement. Behind the
> scenes the machine is chopping your number into digits with division, one
> place at a time, and streaming each digit's character to the same text port.
> You don't see any of that. You hand it a number, it draws a number. But now
> you know *why* there's a separate function: a string is bytes you typed, a
> number is math the machine has to turn into characters on the spot.

> **Tape Jam:** Text not showing up? Two usual culprits. One: you drew it once,
> at startup, but the screen clears every frame and your text only lived for
> that first frame — draw it *every* frame, inside your loop, if you want it to
> stay. Two: you drew it at a coordinate off the edge of the 320×200 screen,
> where it is rendering perfectly into the void. Check your X and Y before you
> blame the port.

> **Try This Before You Panic:** Draw `"DX"` at (10, 10) in white, then draw it
> again at (10, 30) in red — `255, 0, 0`. Two lines, two colors, one screen.
> Now move one of them to X = 400 and watch it vanish, because 400 is past the
> right edge. That's not a bug, that's geography.

---

## Chapter 7 — Reading the Controller

A game nobody can control is a screensaver. Let's read the pad. On the DX the
pattern is always the same three beats: **poll once per frame, then ask
questions.**

```corelx
function Start()
    while true
        wait_vblank()
        input.poll()                 -- read the controller, once, up top
        if input.held(LEFT)
            -- move left while LEFT is down
        if input.pressed(A)
            -- fire ONCE, the instant A goes down
```

`input.poll()` reads the controller hardware and remembers it. Then:

- `input.held(BUTTON)` is true *while* the button is down — for movement, where
  you want continuous action.
- `input.pressed(BUTTON)` is true only on the **single frame** the button goes
  from up to down — for actions you want to happen *once* per press: firing,
  jumping, confirming a menu.
- `input.released(BUTTON)` is the opposite edge — true the frame a button comes
  back up.

The buttons have names you just use: `UP DOWN LEFT RIGHT A B X Y L R START Z`.

> **Fletcher:** The difference between `held` and `pressed` is the thing that
> separates a game that feels right from one that feels broken, so let me make
> it stick. Use `held` for walking: you hold left, you keep walking left, frame
> after frame. Use `pressed` for firing: you press A, *one* shot comes out, and
> you don't get another until you let go and press again. If you wire your fire
> button to `held` by mistake, the player taps A once and unloads forty bullets
> in two-thirds of a second because the button was "down" for forty frames.
> I have shipped that bug. The playtesters called it "the machine gun glitch."
> It was not a feature.

### Why poll first, every frame

> **Field Notes:** `input.pressed` has to know what the button was doing *last*
> frame to spot the edge — was it up a moment ago? CoreLX remembers that for
> you in a corner of memory it owns and you never see. But it can only remember
> correctly if you `poll()` exactly once per frame, at the top of your loop. Poll
> twice and you'll smear two reads together; forget to poll and you're reading a
> stale frame. Once per frame, up top. Make it a habit you don't think about.

### Breaking it on purpose

Wire a fire button to the wrong question:

```corelx
input.poll()
if input.held(A)
    spawn_bullet()      -- a bullet EVERY frame A is down: the machine gun glitch
```

Hold A for one comfortable second and you've spawned sixty bullets. The fix is
one word — `held` becomes `pressed` — and now it's one bullet per press, the way
a person expects.

> **Fletcher's Warning Label:** `held` = "is it down right now." `pressed` =
> "did it *just* go down." Movement wants `held`. Actions want `pressed`. When a
> control feels mushy or machine-gunny, this is the first place to look, and
> nine times out of ten it's the fix.

> **Raccoon Engineering:** You can build a "tap to start, hold to fast-forward"
> control out of these two cheaply: `pressed` triggers the first action
> immediately, and `held` (maybe gated behind a frame counter) takes over if
> they keep the button down. Menus that advance once on tap but scroll when held
> are built on exactly this pair. Two functions, a lot of feel.

> **Try This Before You Panic:** Make a global `var count: int = 0`. In your
> loop, `poll()`, then `if input.pressed(A)` add one to `count`, and draw it
> with `text.draw_int(150, 100, 255, 255, 0, count)`. Mash A and watch it climb
> by exactly one per press. Now change `pressed` to `held` and hold A — watch it
> rocket upward. That runaway number *is* the machine gun glitch, and now you'll
> recognize it on sight. (This is `counter.corelx` in the demo programs below —
> a complete, working version is waiting for you.)

---

## The Demo Programs

Everything you've learned, assembled into complete programs you can build and
run *right now*. These aren't sketches — they live in `docs/manual_examples/`,
and the test suite compiles and runs every one of them against the real
emulator on every build. If a demo here ever stopped working, the build would
break. So they work. Type them in, or load the files, and go.

### `hello.corelx` — words on the glass

The smallest complete program that shows you something. Run it: cyan
`HELLO NITRO` near the middle of the screen.

```corelx
function Start()
    while true
        wait_vblank()
        text.draw(96, 96, 64, 220, 255, "HELLO NITRO")
```

That's the whole thing. One function, one loop, one line of text drawn every
frame. Notice the text is drawn *inside* the loop — draw it once outside and it
flashes for a single frame and vanishes, because the screen clears every frame
(Chapter 6's Tape Jam, made real).

### `counter.corelx` — a number that counts

Press A, the number goes up by one. Hold A, it *still* only goes up by one,
because `input.pressed` fires on the press, not every frame (Chapter 7). This is
the machine-gun-glitch lesson turned into a thing you can hold.

```corelx
var count: int = 0

function Start()
    while true
        wait_vblank()
        input.poll()
        if input.pressed(A)
            count = count + 1
        text.draw(72, 64, 255, 255, 255, "PRESS A TO COUNT")
        text.draw(132, 96, 120, 255, 120, "COUNT")
        text.draw_int(150, 116, 255, 255, 0, count)
```

### `floor.corelx` — walk the floor

The big one: a pseudo-3D floor that rushes toward you as you drive the D-pad. It
pulls together everything in this guide so far — a tile asset, the matrix-plane
setup, the projection and camera builtins, global state, `input.held` movement,
signed clamping to keep you inside the world, and a HUD. It is, deliberately, a
small version of the kind of game the DX was built to make.

```corelx
asset Floor: tiles8 hex
    11 11 22 22 11 11 22 22
    11 11 22 22 11 11 22 22
    22 22 11 11 22 22 11 11
    22 22 11 11 22 22 11 11
    11 11 22 22 11 11 22 22
    11 11 22 22 11 11 22 22
    22 22 11 11 22 22 11 11
    22 22 11 11 22 22 11 11

const MOVE = 6

var cam_x: int = 512
var cam_y: int = 768

function Start()
    gfx.init_default_palettes()
    bg.enable(0)
    bg.bind_transform(0, 0)
    matrix.enable(0)
    matrix.identity(0)
    matrix_plane.enable(0, 128)
    matrix_plane.load_tiles(ASSET_Floor, 0, 0)
    matrix_plane.clear(0, 1, 0)
    matrix_plane.set_projection(0, 1, 113)
    matrix_plane.set_depth(0, 0x0C00, 0xC000, 0x00C0)
    ppu.enable_display()

    while true
        wait_vblank()
        input.poll()
        if input.held(UP)
            cam_y = cam_y - MOVE
        if input.held(DOWN)
            cam_y = cam_y + MOVE
        if input.held(LEFT)
            cam_x = cam_x - MOVE
        if input.held(RIGHT)
            cam_x = cam_x + MOVE
        if cam_x < 0
            cam_x = 0
        if cam_x > 1023
            cam_x = 1023
        if cam_y < 0
            cam_y = 0
        if cam_y > 1023
            cam_y = 1023
        matrix_plane.set_camera(0, cam_x, cam_y, 0, 256)
        text.draw(8, 8, 255, 255, 255, "WALK THE FLOOR")
        text.draw(8, 184, 160, 200, 255, "DPAD TO MOVE")
```

> **Fletcher:** Read that last one top to bottom and notice there's nothing in
> it you haven't already met. Setup happens once, before the loop. The loop runs
> forever: wait for the frame, read the pad, move, *clamp so you can't walk off
> the edge of the world*, push the camera to the plane, draw the HUD. That
> shape — setup, then `while true` of poll/update/draw — is the skeleton under
> every game on this machine. Learn that rhythm and the rest is just filling in
> what happens in the middle.

---

*Chapters land here as their features get built and verified on the machine.
Next in the workshop: sprites that move, and more of what the matrix planes can
do once you start tilting the whole world.*
