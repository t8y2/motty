// Render the unmodified DBX SVGs, including use, markers, strokes and clipping.
// This maintainer tool is not part of the Go application or its startup.
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const root = new URL('../../internal/workbench/flags/', import.meta.url);
mkdirSync(new URL('png/', root), { recursive: true });
const hashes = {};
const hash = (data) => createHash('sha256').update(data).digest('hex');
// Compact flag sources use marker-mid even on invisible paths. Use a complete
// browser SVG renderer, so every marker, fill, stroke and clip is retained.
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : { channel: 'chrome' }) });
try {
  const page = await browser.newPage({ viewport: { width: 128, height: 96 }, deviceScaleFactor: 1 });
  for (const name of readdirSync(new URL('svg/', root)).filter((name) => name.endsWith('.svg')).sort()) {
    const svg = readFileSync(new URL(`svg/${name}`, root));
    await page.setContent(`<style>html,body{margin:0}svg{display:block;width:128px;height:96px}</style>${svg.toString('utf8')}`);
    const png = await page.locator('svg').screenshot({ omitBackground: true, animations: 'disabled' });
    writeFileSync(new URL(`png/${name.replace(/\.svg$/, '.png')}`, root), png);
    hashes[name] = { svg: hash(svg), png: hash(png) };
  }
  writeFileSync(new URL('manifest.json', root), JSON.stringify({ renderer: `Chromium ${browser.version()} (playwright-core@1.56.1)`, width: 128, files: hashes }, null, 2) + '\n');
  console.log(`Rendered ${Object.keys(hashes).length} DBX flags in ${fileURLToPath(root)}.`);
} finally {
  await browser.close();
}
