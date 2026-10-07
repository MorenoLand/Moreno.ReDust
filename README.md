# A Go reconstruction of DUST

ReDust is a work-in-progress Go and Ebitengine port of the 1995 adventure game DUST. It reconstructs game behavior from the original engine and local game data.

## Game data

Original game assets are not included. Place legally obtained files in `bin/assets/`, preserving their original relative paths. A different work directory can be selected with `--work`; assets are read from its `assets` child. See [assets/README.md](assets/README.md).

## Run

```powershell
go run . --debug --work=bin/
```

For a headless initial screenshot:

```powershell
go run . --silent --debug --work=bin/
```

The screenshot is written to `bin/redust-silent.png`. Use `--load=bin/example.redust.json` to start from a ReDust save.

For scripted headless input and screenshots:

```powershell
go run . --work=bin/ --silent-script=bin/check.json --silent-seed=1
```

The script runs the game's input callbacks with audio muted. It accepts `key`, `click`, `mouse_down`, `mouse_up`, `mouse_move`, `wait`, `wait_until`, `assert`, and `snapshot` actions; snapshots must be PNGs inside the project's `bin/` directory. For example:

```json
{"actions":[{"type":"key","key":"Space"},{"type":"wait","milliseconds":500},{"type":"snapshot","path":"bin/check.png"}]}
```

Use `assert` with an `expect` object to check game state. `wait_until` takes the same object and a `milliseconds` timeout (up to 60000), updating the game until it matches. Dialogue state includes `conversation.open`, `conversation.choosing`, `conversation.choiceEvents`, and `conversation.taskActive`.

## Build

```powershell
.\tools\build.ps1 -Target pc
.\tools\build.ps1 -Target wasm
```

`pc` writes `bin/redust.exe`. `wasm` writes the browser bundle to `bin/web/`; serve that directory over HTTP to run it in a browser. Use `-Target all` to build both.

The port is incomplete; native gameplay behavior is still being reconstructed and verified.
