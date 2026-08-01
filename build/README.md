# Wails 3 build assets

The Wails 3 Taskfiles in this directory build and package PSNWDL on Windows,
macOS, and Linux. Product metadata and development-mode settings live in
`config.yml`; platform-specific packaging assets live in the matching
subdirectory. Build output is written to the repository-level `bin` directory.

Run `wails3 task common:update:build-assets` after changing product metadata.
That command regenerates platform files, so review its changes before committing.
Use `build/appicon.png` as the source for generated Windows and macOS icons.
