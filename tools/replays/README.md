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
