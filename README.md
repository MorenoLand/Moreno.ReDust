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

The screenshot is written to `bin/redust-silent.png`. Silent mode exits before the interactive game and audio start.

## Build

```powershell
.\tools\build.ps1 -Target pc
.\tools\build.ps1 -Target wasm
```

`pc` writes `bin/redust.exe`. `wasm` writes the browser bundle to `bin/web/`; serve that directory over HTTP to run it in a browser. Use `-Target all` to build both.

The port is incomplete; native gameplay behavior is still being reconstructed and verified.
