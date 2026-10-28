# Language flag assets

The SVG files in `internal/workbench/flags/svg/` are copied unchanged from DBX.
Generate their bundled images with:

```sh
cd tools/flags
npm ci
npm run generate
```

The tool uses installed Google Chrome (or `CHROME_PATH`) through the pinned
Playwright API. The browser's complete SVG renderer preserves references,
markers, strokes, clipping and self-intersecting fill rules. Images are generated
at 128 × 96 pixels to cover
desktop display scaling. Neither Node.js nor the renderer is needed to build or
run Motty; the Go application embeds the PNGs and decodes them once on demand.
`manifest.json` records the source and generated hashes.

Traditional Chinese uses DBX's “繁” text marker and has no flag asset.
