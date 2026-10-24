# Headless replays

`courtyard-door-snapshots.json` checks the courtyard door end to end: it loads a save in
the courtyard, clicks the door, leaves by the up arrow, and writes snapshots under `bin/`.

Build the save first, then run the replay (the save and snapshots are scratch output):

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -out=bin/courtyard.json
go run . --silent --debug --work=bin/ --load=bin/courtyard.json --silent-script=tools/replays/courtyard-door-snapshots.json
```

The replay asserts the set is `court` after the click and `town` after the exit. The
snapshots show the open door and the street outside. `tools/setviews` lists the views of
any set, for choosing another save point.

`courtyard-night-snapshots.json` is the same check at night. Its save comes from the same
tool with `-set=nitecour -clock=3`, which is the night courtyard file (its rows are named
`court`, so the port reports the set as `court`):

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=nitecour -clock=3 -out=bin/courtyard-night.json
go run . --silent --debug --work=bin/ --load=bin/courtyard-night.json --silent-script=tools/replays/courtyard-night-snapshots.json
```

`chin-door-exit.json` checks the Chinese restaurant's rice door the same way: the save starts
in Scene A2 facing west, the click opens the door, and the up arrow leads out to the town.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=chin -scene="Scene A2" -point=0,1,4 -out=bin/chin.json
go run . --silent --debug --work=bin/ --load=bin/chin.json --silent-script=tools/replays/chin-door-exit.json
```

`store-door-exit.json` does the same for the general store (its door is in Scene D2, facing east):

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=store -scene="Scene D2" -point=3,1,3 -out=bin/store.json
go run . --silent --debug --work=bin/ --load=bin/store.json --silent-script=tools/replays/store-door-exit.json
```

Note: courtsave sets the town return scene to Scene G8, so every test exit lands on that street.
Real saves record the scene the player entered from.

`underground-open.json` needs a save in the snake set. Its first background row is Scene B2 facing west:

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=snake -scene="Scene B2" -point=1,1,1 -out=bin/snake.json
go run . --silent --debug --work=bin/ --load=bin/snake.json --silent-script=tools/replays/underground-open.json
```

The target range (`TARGET/TARGET.SET`) opens from its first row, Scene I13, facing west
(`-point=8,12,1`). Its openset expects the game's entry script to have set the path first,
so a save-started open reports `openstagefile` status 0x2a, which is a test artifact.

`hub-sundial-chest.json` turns the hub's sundial dials to 12, 4, 12 and leaves by the EXIT button. That
is the chest branch of exitsundial: the phase becomes 3 and CHESTAPP.MOV plays. The save starts in the hub
(Scene C4) at phase 2, as the courtsave command sets it.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=hub -scene="Scene C4" -point=2,3,3 -out=bin/hub-chest.json
go run . --silent --debug --work=bin/ --load=bin/hub-chest.json --silent-script=tools/replays/hub-sundial-chest.json
```

`target-shot.json` fires once at the target range. The save holds the gun in hand with six bullets
(courtsave's `-gun`), and the click at (256,140) runs the shot. The replay asserts that one shot was
counted.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=target -scene="Scene I13" -point=8,12,1 -path6="dust:target:" -gun -out=bin/target-hold.json
go run . --silent --debug --work=bin/ --load=bin/target-hold.json --silent-script=tools/replays/target-shot.json
```

`target-exit.json` leaves the range by its EXIT button (held for a moment, as a click is), on the same gun
save as `target-shot.json`. The range's score is given and the set returns to the town.

```bash
go run . --silent --debug --work=bin/ --load=bin/target-hold.json --silent-script=tools/replays/target-exit.json
```

`hub-mine-entry.json` runs the hub's mine route from its start: the sundial's three dials to 4, 8 and 12,
the EXIT (which sets nextroom to mine, since the mine puzzle is not done), the walk through C4, C3, D3 and D2
to Scene D1 facing north, one up-arrow to start the blackout, then keys to count it down. The mine set opens.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=hub -scene="Scene C4" -point=2,3,3 -out=bin/hub-mine-entry.json
go run . --silent --debug --work=bin/ --load=bin/hub-mine-entry.json --silent-script=tools/replays/hub-mine-entry.json
```

`mine-puzzle.json` solves the mine. From the hub with the mask in hand (courtsave's `-hand=mask`), it runs the
hub-mine-entry route into the mine, then the mine's wrap-around grid: the skeleton starts 15 cells east and 14 cells
south of the centre (standx 16000, standy 14976), so the player walks to C3 and wraps the grid 15 times east and 14
times south. The asserts check standx and standy reach 640. Then the mask is dragged from the panel onto the skeleton,
which sets minepuzzle to done, plays SKELETON.MOV and returns to the hub.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=hub -scene="Scene C4" -point=2,3,3 -hand=mask -out=bin/hub-mine-mask.json
go run . --silent --debug --work=bin/ --load=bin/hub-mine-mask.json --silent-script=tools/replays/mine-puzzle.json
```

`snake-puzzle.json` solves the snake room's hex puzzle. It opens the puzzle from the south view of Scene B3 (the
hex glyph at 350,162), which plays HEXOPEN.MOV and loads the seven wheels from SNAKE.PRP with mangle(). A wheel turns one
notch per clockwise drag, and it moves its neighbours by their ring index (the neighbours' distances, 50 to 100 is one
ring, 100 to 150 two, over 150 three). From the start state the 14 drags below solve it: stop4 twice, stop5 five times,
stop6 twice and stop7 five times. The replay checks every wheel reads 0, presses the solve button at 413,202, and asserts
snakepuzzle is done and the set returns to the hub.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=snake -scene="Scene B3" -point=1,2,2 -out=bin/snake-puzzle.json
go run . --silent --debug --work=bin/ --load=bin/snake-puzzle.json --silent-script=tools/replays/snake-puzzle.json
```

`fountain-to-hub.json` runs the courtyard fountain route: the save is in Scene C5 facing north, day 4 at night, with the tstone item in the box. The click plays OPENFOUN.MOV and the game reaches the hub (Scene D7).

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=court -scene="Scene C5" -point=2,4,1 -clock=3 -day=4 -out=bin/fountain.json
# then set inventoryOwners.tstone to "box" in bin/fountain.json
go run . --silent --debug --work=bin/ --load=bin/fountain.json --silent-script=tools/replays/fountain-to-hub.json
```

`bank-crack.json` opens the bank's safe. The save is in the bank's Scene D1 facing west, on day 3 at night.
Clicking the sign runs docrack, which opens the crack stage with its dial and combination buttons.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=bank -scene="Scene D1" -point=3,0,4 -clock=3 -day=3 -out=bin/bank.json
go run . --silent --debug --work=bin/ --load=bin/bank.json --silent-script=tools/replays/bank-crack.json
```

`bank-crack-exit.json` continues the safe: the same click, then the EXIT button. The button's script runs
giveup(), which closes the crack stage and returns to the bank's vault door. It uses the bank save above.

`bank-crack-solve.json` opens the safe the whole way, on the same bank save. It turns the dial
three times to the combination 08, 23, 41 and clicks OPEN. The dial's drags are clockwise to
08, counterclockwise to 23, and clockwise to 41, each ending on the number the dial lands on
when released. The replay asserts the combination after each turn and the set after the vault
movie. The first turn needs about 300 degrees of drag: the dial starts at 0 and only turns
right on that turn, so each number is reached by going around.

```bash
go run . --silent --debug --work=bin/ --load=bin/bank.json --silent-script=tools/replays/bank-crack-solve.json
```

`tbird-puzzle.json` solves the tumbler lock in the tbird room. The save is at the podium in Scene B2 facing north,
with the tbird in hand. The replay clicks the podium, drops the tbird on the tumble flat (its offer runs gotoflat("tumble1"),
TUMOPEN.MOV and gotoflat("tumble2")), and turns the five tumblers to 8, 8, 4, 20 and 12. A click in a tumbler's lower
half (y at least 100) moves its degree forward 4 and a click in its upper half moves it back 4. The EXIT button then runs
gototbird(): tbirdpuzzle becomes done, TUMTRAN.MOV plays and the hub opens at Scene D7.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=tbird -scene="Scene B2" -point=1,1,1 -hand=tbird -out=bin/tbird-hand.json
go run . --silent --debug --work=bin/ --load=bin/tbird-hand.json --silent-script=tools/replays/tbird-puzzle.json
```

`flute-puzzle.json` solves the flute room. The save is the hub's Scene G4 facing east, with the flute in hand and
nextroom set to flute (courtsave's `-room`, the value the sundial's exit leaves for the flute arch). The replay goes
up the arch (uparrow starts the blackout, the keys count it down), which opens flute.set at Scene B4 facing north.
It clicks the flute, drops it on flat 0 (its offer runs gotoflat("flat 1")), presses buttons 1 to 5 in order, which solves the
room: FLUTE.MOV plays and the hub opens.

```bash
go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -set=hub -scene="Scene G4" -point=6,3,3 -hand=flute -room=flute -out=bin/hub-flute.json
go run . --silent --debug --work=bin/ --load=bin/hub-flute.json --silent-script=tools/replays/flute-puzzle.json
```
