# Local game assets

ReDust does not distribute DUST's original game data. Supply files from a legally obtained copy under `bin/assets/`, preserving the original directory and file names. For another work directory, place the `assets` directory beneath it and pass that directory with `--work`.

The PC run reads assets from `<work>/assets`. The WebAssembly build copies those assets to `bin/web/assets`. Do not add original art, audio, movies, or executables to this repository.
