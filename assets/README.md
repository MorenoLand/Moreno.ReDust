# Locally extracted runtime data

Place files extracted from the user's legally obtained DUST copy in `bin/assets/`. `--work` selects any OS-visible filesystem directory, including another drive or mounted folder; its `assets/` child is the game content root, defaulting to `bin/assets/`. For WebAssembly, stage the same locally extracted data in `bin/web/assets/` beside the web bundle. Original executables, game data, audio, video, images, scripts, and archives are excluded from public source distribution.
