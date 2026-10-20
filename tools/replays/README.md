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
