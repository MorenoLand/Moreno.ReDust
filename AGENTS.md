# ReDust Reverse-Engineering Rules

## Reference boundary

- Authoritative reference: user-supplied `DF386.EXE`.
- Required SHA-256: `30d34940f218091a70634fcc8fb27837b7dfd1360c880d3fcc9ce0f50ea47c0f`.
- User-exported analyzed archive: `../DF386.EXE.gzf`.
- Active isolated Ghidra project: `Ghidra/DUSTGZF`.
- ReAgent evidence, manifests, reports, logs, and sessions: `.codex/`.
- Reconstructed code output: root `main.go` and `go.mod`, with implementation grouped into domain packages such as `engine/`, `scripts/`, `render/`, and `audio/`.
- Desktop/runtime executable output: root `bin/`; runtime assets are in `bin/assets/` beside the executable.
- Runtime data is supplied locally by the user; it is never redistributed by this project.

Keep `Original/DF386.EXE` pristine. Any runtime patch or compatibility experiment must use a scratch copy and must not replace the reference input.

## Legal asset boundary

- The public project contains code, schemas, tools, and documentation only.
- Users must extract their own legally obtained DUST files into `bin/assets/` before running the packaged executable.
- Never commit, package, embed, encode, or publish original executables, APPL containers, PUP/SET/CST/FLT/PRP/SND/MOV data, palettes, portraits, music, dialogue, or other copyrighted game assets.
- `--work` selects any OS-visible filesystem directory, including another drive or a locally mounted/network folder; its `assets/` child is the content root. Default it to `bin/`, making `go run .` use `bin/assets/`.
- Support `--debug` for diagnostics. Do not make normal output noisy.
- Use relative paths and never hard-code a local installation path.
- Tests that require extracted assets must detect missing work-directory assets and report the prerequisite without bundling replacement content.

## Cross-platform runtime architecture

- Implement the reimplementation in Go with Ebitengine; keep Windows, Linux, macOS, and WebAssembly in scope from the first runtime slice.
- Keep `main.go` and `go.mod` at the repository root so `go run .` works. Put implementation in domain packages such as `engine/`, `scripts/`, `render/`, and `audio/`; do not add `src/`, `cmd/`, or `internal/` wrapper trees.
- Keep platform-neutral game logic, APPL/container decoding, script interpretation, timing, state, and rendering data in those domain packages; isolate window, input, audio, and presentation behind Ebitengine.
- Do not carry Win32, WinG, GDI, DirectDraw, or DOS APIs into the portable core.
- Preserve the original software-rendering behavior through Ebitengine images, pixel writes, palette handling, and shaders only where evidence requires them; do not invent visual behavior.
- Make asset roots, display mode, audio backend, and input mappings runtime-configurable. Native `--work` accepts any OS-visible path; WebAssembly reads user-staged assets from an `assets/` directory adjacent to the web bundle without embedding them in public source.
- Development launch: `go run .`; `--debug` enables diagnostics; `--work=bin/` selects the default runtime work directory.
- Final native executable goes in `bin/`, beside `bin/assets/`; WebAssembly output and its locally staged assets go in `bin/web/`. Keep generated binaries and extracted data out of public commits.

## Required analysis order

1. Verify the reference hash before using evidence.
2. Confirm the PE32 imagebase is `0x00400000`; do not relocate the program.
3. Trace the CRT boundary from the `R600x` strings before counting engine functions.
4. Resolve the opcode name table at `0x0045EB08` and determine whether dispatch compares text or indexes integers.
5. Decode the APPL word-swapped strings before interpreting script/data records.
6. Use ReAgent for bounded function evidence and candidate C/C++ recovery.
7. Translate only verified behavior into grouped Go subsystem packages; native C/C++ candidates are evidence, not runtime source.

Do not invent interpreter behavior, file formats, timing semantics, or ownership. Treat unresolved behavior as an explicit evidence gap.

## Public-repository and checkpoint rules

- This project is intended for public publication. Never write absolute drive paths, usernames, home paths, private tool locations, credentials, or local archive paths into tracked source, documentation, configuration, or commit messages.
- Use repository-relative paths and environment variables such as `GHIDRA_INSTALL_DIR`.
- Keep `.codex/`, `Ghidra/`, `*.gzf`, logs, generated evidence, manifests, and local runtime data ignored or untracked.
- Make frequent factual, GPG-signed local commits at meaningful conversion milestones so the work has restore points. Do not push unless explicitly requested.

## ReAgent workflow

Run from the ReDust repository root:

```powershell
$re = '.\.codex\venv\Scripts\re-agent.exe'
$bridge = '.\.codex\venv\Scripts\ghidra-bridge.exe'
& $bridge --config '.\.codex\ghidra-bridge.yaml' export decompiled
& $re --config '.\.codex\re-agent.yaml' doctor --address 0xADDRESS
& $re --config '.\.codex\re-agent.yaml' plan --address 0xADDRESS --max-depth 0 --max-functions 1 --output '.\.codex\target-plan.json'
& $re --config '.\.codex\re-agent.yaml' reverse --manifest '.\.codex\target-plan.json' --max-functions 1 --max-rounds 1
```

Validation is disabled until a real DUST candidate-consuming native/differential harness exists. Generated candidates are evidence, not completed Go implementation or parity proof. Run Go development with `go run .`; place final build output in `bin/`.
