/**
 * Platform post-build script for GraphCommerce projects, paired with
 * `cache-handler.mjs` (same directory in ho-nl/deployyy — the build workflow
 * downloads both). Converts the pages `next build` prerendered in
 * .next/server/pages/ into the cache handler's on-disk format and packs them
 * into cache-seed.tar.gz, so a deploy can pre-warm the shared cache volume
 * and the first visitors after a rollout do not pay a render.
 *
 * Runs inside the Docker builder stage after `yarn build`. The entry format
 * MUST match cache-handler.mjs: plain JSON files named <md5(key)>.json under
 * data/<BUILD_ID>/, plus a build marker under _builds/.
 */

import { readFileSync, writeFileSync, mkdirSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, relative } from 'node:path'
import { createHash } from 'node:crypto'
import { execSync } from 'node:child_process'

const NEXT_DIR = join(process.cwd(), '.next')
const BUILD_ID = readFileSync(join(NEXT_DIR, 'BUILD_ID'), 'utf8').trim()
const PAGES_DIR = join(NEXT_DIR, 'server', 'pages')
const OUTPUT_DIR = join(process.cwd(), 'cache-seed')

function md5(str) {
  return createHash('md5').update(str).digest('hex')
}

function findPages(dir) {
  const results = []

  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      results.push(...findPages(full))
    } else if (entry.name.endsWith('.html')) {
      const base = entry.name.slice(0, -5)
      const jsonPath = join(dir, `${base}.json`)
      if (existsSync(jsonPath)) {
        const rel = relative(PAGES_DIR, join(dir, base))
        const key = `/${rel.replace(/\\/g, '/')}`
        results.push({ key, htmlPath: full, jsonPath })
      }
    }
  }

  return results
}

const pages = findPages(PAGES_DIR)

const dataDir = join(OUTPUT_DIR, 'data', BUILD_ID)
const buildsDir = join(OUTPUT_DIR, '_builds')
mkdirSync(dataDir, { recursive: true })
mkdirSync(buildsDir, { recursive: true })

writeFileSync(join(buildsDir, BUILD_ID), Date.now().toString())

let count = 0
let totalSize = 0

for (const page of pages) {
  const html = readFileSync(page.htmlPath, 'utf8')
  const pageData = JSON.parse(readFileSync(page.jsonPath, 'utf8'))

  const data = { kind: 'PAGES', html, pageData, headers: {}, status: 200 }
  const json = JSON.stringify(data)
  const hash = md5(page.key)

  writeFileSync(join(dataDir, `${hash}.json`), json)
  count++
  totalSize += json.length
  console.log(`  ${page.key} → ${hash}.json (${(json.length / 1024).toFixed(1)}KB)`)
}

console.log(`\nPacked ${count} pages (${(totalSize / 1024).toFixed(0)}KB) for build ${BUILD_ID}`)

execSync(`tar -czf cache-seed.tar.gz -C cache-seed .`)
const tarSize = statSync('cache-seed.tar.gz').size
console.log(`Created cache-seed.tar.gz (${(tarSize / 1024).toFixed(0)}KB)`)
