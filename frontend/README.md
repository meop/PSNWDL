# PSNWDL frontend

This is the Svelte 5 and TypeScript frontend embedded by the Wails 3 desktop
application. Generated Go service and model bindings live in `bindings/`.

- `pnpm run check` runs the Svelte and TypeScript diagnostics.
- `pnpm run build` creates the production bundle in `dist/`.
- `wails3 generate bindings -clean=true -ts` regenerates bindings from the Go
  module root.

Normally use `wails3 dev` or `wails3 build` from the repository root so binding
generation and frontend compilation happen in the correct order.
