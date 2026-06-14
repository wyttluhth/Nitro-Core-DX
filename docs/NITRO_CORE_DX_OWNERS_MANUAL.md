# Nitro-Core-DX — Owner's Manual

### Welcome to the console that should have existed

**A Retro Code Ramen publication.** Thank you for choosing Nitro-Core-DX.

> This is the **owner's manual** — everything you need to start the system and
> play. If you want to *make* games for the Nitro-Core-DX, you want the
> **Programming Guide** (`CORELX_PROGRAMMING_GUIDE.md`) instead, where Fletcher
> will teach you the CoreLX language and the DevKit. This book is for players.

---

## 1. Welcome

Nitro-Core-DX is a 16-bit fantasy console — a machine from an alternate timeline
where the best ideas of the 16-bit era all landed in one box. It pairs
SNES-style graphics (lush color, Mode 7-style perspective effects) with
Genesis-style speed (a fast CPU, smooth 60 frames per second). It was designed
from the ground up to do the things the old machines could only *almost* do:
tilt a whole floor into the distance, scroll layered worlds, and never drop a
frame doing it.

You don't need to know any of that to enjoy it. You need to know how to turn it
on, hold the controller, and load a game. That's this chapter and the next two.

---

## 2. Getting Started

The Nitro-Core-DX runs on the desktop emulator (Linux, Windows, and more). To
start a game:

```
./emulator -rom your_game.rom
```

That's it. A window opens, the system boots, and your game runs. Games come as
**`.rom` files** — single cartridge files that hold an entire game. Point the
emulator at one and play.

If a game ships with the system, you'll find it in the `roms/` folder. Try one
to confirm everything's working:

```
./emulator -rom roms/nitro_pack_in_demo.rom
```

You should see a title screen. Press **START** to begin.

---

## 3. The Controller

The Nitro-Core-DX controller is a classic layout — comfortable in the hand,
nothing you have to think about.

```
       _____________________________
      /                             \
     |  [L]                     [R]  |
     |   [↑]               (X)       |
     | [←][→]           (Y)   (A)    |
     |   [↓]               (B)       |
     |        [Z]  [START]           |
      \_____________________________/
```

| Control | What it does (in most games) |
|---------|------------------------------|
| **D-Pad** (↑ ↓ ← →) | Move. Navigate menus. |
| **A** | Confirm / jump / primary action |
| **B** | Cancel / secondary action |
| **X**, **Y** | Extra actions (game-specific) |
| **L**, **R** | Shoulder buttons (game-specific) |
| **START** | Start the game / pause |
| **Z** | Menu / mode switch (game-specific) |

Exactly what each button does is up to each game — but **START** to begin and
the **D-Pad** to move are nearly universal. When in doubt at a title screen,
press START.

---

## 4. What Your System Can Do

You bought a small machine with a lot of muscle. Here's what's under the hood,
in plain terms:

- **Gorgeous color.** The screen is 320×200 pixels, and the system can draw from
  a palette of **32,768 colors** — that deep, rich 16-bit look.
- **Layered worlds.** **Four independent background layers** stack and scroll at
  their own speeds, which is how games get that sense of depth as you move.
- **Matrix Mode.** This is the showpiece: Mode 7-style perspective that can tilt
  a floor or a wall into the distance, rotate it, and rush it toward you — and
  unlike the old machines, it can do this on *multiple* layers at once. Racing
  games, pseudo-3D adventures, and sweeping landscapes all live here.
- **128 sprites.** Plenty of moving characters, bullets, and effects on screen
  at once, with priority and blending so things layer correctly.
- **Real speed.** A roughly 7.67 MHz CPU — close to three times the SNES's —
  driving a steady **60 frames per second**. Fast games feel fast.
- **Full sound.** Multiple audio channels with an FM synthesis path for that
  warm, punchy retro soundtrack.

---

## 5. Care, Comfort, and Common Questions

It's a fantasy console, so you won't be blowing dust out of cartridges — but a
few real things still apply.

**Take breaks.** Sixty frames a second is smooth and easy on the eyes, but any
screen deserves a rest now and then. Look at something far away every so often.

**"The game won't start."** Make sure you pointed the emulator at a real `.rom`
file and that the path is spelled correctly. If the window opens but the screen
is black, press START — many games wait on their title screen.

**"It's a black screen and nothing happens."** Confirm the ROM file isn't empty
or corrupted, and that you're running a Nitro-Core-DX `.rom` (not a file from a
different system). Try a known-good ROM from the `roms/` folder to check the
system itself is fine.

**"The controls feel off."** Different games map the buttons differently. Check
that game's own instructions. The D-Pad moves and START begins in almost
everything.

---

## 6. Where to Go Next

Want to make your own games? You can. The Nitro-Core-DX was built to be
programmed — that's the whole point — and the **Programming Guide**
(`CORELX_PROGRAMMING_GUIDE.md`) will take you from your first line of CoreLX to a
playable pseudo-3D demo, with Fletcher grumbling helpfully the whole way.

Welcome to the machine. Now go play something.

— Retro Code Ramen

---

## Appendix — Technical Specifications

For the curious. None of this is required reading to play.

| Feature | Specification |
|---------|--------------|
| Display resolution | 320×200 pixels (landscape) / 200×320 (portrait) |
| Color palette | 256-color CGRAM, RGB555 format (32,768 possible colors) |
| Tile size | 8×8 or 16×16 pixels (per layer) |
| Max sprites | 128 |
| Background layers | 4 independent (BG0–BG3) |
| Matrix Mode | Mode 7-style per-layer transforms, perspective, direct color |
| Audio | Multiple channels + YM2608-capable FM path |
| Audio sample rate | 44,100 Hz |
| CPU speed | ~7.67 MHz (127,820 cycles per frame at 60 FPS) |
| Memory | 64KB per bank, up to 256 banks |
| ROM size | Up to ~7.8 MB cartridges |
| Frame rate | 60 FPS |

These figures describe the Nitro-Core-DX as implemented by the emulator, which
is the authoritative reference for the system's behavior.
