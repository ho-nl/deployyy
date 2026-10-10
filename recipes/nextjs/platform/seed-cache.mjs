/**
 * Platform post-build script for Next.js and GraphCommerce projects, paired
 * with `cache-handler.mjs` (same directory). Converts every
 * route `next build` prerendered into the cache handler's on-disk format and
 * packs them into cache-seed.tar.gz. The build ships the tarball as a
 * separate `<tag>-cache-seed` image; the operator's per-build seed Job
 * (deployyy-operator internal/platform/graphcommerce/seedjob.go) extracts it
 * onto the shared cache volume, so a release starts warm.
 *
 * Runs in the Docker builder stage after `next build`, in the project root.
 *
 * The entry format MUST match cache-handler.mjs:
 *   data/<BUILD_ID>/<md5(key)>.json   plain JSON, no gzip (the volume is
 *                                     btrfs with transparent zstd)
 *   _builds/<BUILD_ID>                build marker; the seed Job reads the
 *                                     build id from this tar entry
 * Buffers are stored as {"__b": base64} and Maps as {"__m": entries}, the
 * handler's own replacer format.
 *
 * The route list comes from .next/prerender-manifest.json, the build's own
 * record of what it prerendered. The value shape follows the Next major the
 * project builds with, because Next renamed the cache kinds in 15:
 *   Next 13/14: { kind: 'PAGE', html, pageData }    pageData = JSON (pages
 *               router) or the RSC payload as a string (app router)
 *   Next 15+:   { kind: 'PAGES', html, pageData }   pages router
 *               { kind: 'APP_PAGE', html, rscData, segmentData, ... }
 * A seeded entry with the wrong kind would be worse than no seed, so an
 * unknown Next major seeds nothing and says so.
 *
 * Not seeded: route handlers (APP_ROUTE, cheap to render), fetch cache
 * entries (they fill on first use) and images (the Bunny perma-cache owns
 * /_next/image at the edge).
 */

import { readFileSync, writeFileSync, mkdirSync, statSync, existsSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { execFileSync } from 'node:child_process'

const ROOT = process.cwd()
const NEXT_DIR = join(ROOT, '.next')
const BUILD_ID = readFileSync(join(NEXT_DIR, 'BUILD_ID'), 'utf8').trim()
const OUTPUT_DIR = join(ROOT, 'cache-seed')
const TARBALL = join(ROOT, 'cache-seed.tar.gz')

const nextVersion = createRequire(join(ROOT, 'package.json'))('next/package.json').version
const nextMajor = parseInt(nextVersion, 10)

function md5(str) {
  return createHash('md5').update(str).digest('hex')
}

function readJson(path) {
  return existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : null
}

const b64 = (buf) => ({ __b: buf.toString('base64') })

/** One prerendered route -> the handler value, or null when not seedable. */
function entryFor(route) {
  const file = route === '/' ? 'index' : route.slice(1)
  const appBase = join(NEXT_DIR, 'server', 'app', file)
  const pagesBase = join(NEXT_DIR, 'server', 'pages', file)

  if (existsSync(`${appBase}.html`)) {
    const html = readFileSync(`${appBase}.html`, 'utf8')
    const meta = readJson(`${appBase}.meta`) ?? {}
    const rscPath = `${appBase}.rsc`
    if (!existsSync(rscPath)) return null
    const rsc = readFileSync(rscPath)
    if (nextMajor >= 15) {
      let segmentData
      if (Array.isArray(meta.segmentPaths)) {
        const entries = []
        for (const segmentPath of meta.segmentPaths) {
          const p = join(`${appBase}.segments`, `${segmentPath}.segment.rsc`)
          if (existsSync(p)) entries.push([segmentPath, b64(readFileSync(p))])
        }
        segmentData = { __m: entries }
      }
      return {
        kind: 'APP_PAGE',
        html,
        rscData: b64(rsc),
        postponed: meta.postponed,
        headers: meta.headers,
        status: meta.status,
        segmentData,
      }
    }
    return {
      kind: 'PAGE',
      html,
      pageData: rsc.toString('utf8'),
      postponed: meta.postponed,
      headers: meta.headers,
      status: meta.status,
    }
  }

  if (existsSync(`${pagesBase}.html`) && existsSync(`${pagesBase}.json`)) {
    return {
      kind: nextMajor >= 15 ? 'PAGES' : 'PAGE',
      html: readFileSync(`${pagesBase}.html`, 'utf8'),
      pageData: JSON.parse(readFileSync(`${pagesBase}.json`, 'utf8')),
      headers: {},
      status: 200,
    }
  }
  return null
}

rmSync(OUTPUT_DIR, { recursive: true, force: true })
const dataDir = join(OUTPUT_DIR, 'data', BUILD_ID)
const buildsDir = join(OUTPUT_DIR, '_builds')
mkdirSync(dataDir, { recursive: true })
mkdirSync(buildsDir, { recursive: true })
writeFileSync(join(buildsDir, BUILD_ID), Date.now().toString())

let count = 0
let totalSize = 0

if (nextMajor < 13 || nextMajor > 16) {
  console.log(
    `::warning::The cache seed supports Next 13 to 16, not Next ${nextVersion}. The release starts with an empty page cache.`,
  )
} else {
  const manifest = readJson(join(NEXT_DIR, 'prerender-manifest.json')) ?? { routes: {} }
  for (const route of Object.keys(manifest.routes ?? {})) {
    if (route === '/_not-found' || route === '/404' || route === '/500') continue
    const value = entryFor(route)
    if (!value) continue
    const key = route === '/' ? '/index' : route
    const json = JSON.stringify(value)
    writeFileSync(join(dataDir, `${md5(key)}.json`), json)
    count++
    totalSize += json.length
    console.log(`  ${key} (${value.kind}) -> ${md5(key)}.json (${(json.length / 1024).toFixed(1)}KB)`)
  }
}

console.log(`\nPacked ${count} routes (${(totalSize / 1024).toFixed(0)}KB) for build ${BUILD_ID}, Next ${nextVersion}`)

// The tarball itself is gzip-compressed for transport only; the entries it
// holds are plain JSON, as the handler writes them.
execFileSync('tar', ['-czf', TARBALL, '-C', OUTPUT_DIR, '.'])
const tarSize = statSync(TARBALL).size
console.log(`Created cache-seed.tar.gz (${(tarSize / 1024).toFixed(0)}KB)`)

// The seed ships as its own image, so it never slows an app pull. It still
// slows the seed Job and fills the per-env cache volume (20Gi). Warn well
// before that hurts.
const SEED_WARN_MB = parseInt(process.env.CACHE_SEED_WARN_MB || '512', 10)
if (tarSize > SEED_WARN_MB * 1024 * 1024) {
  console.log(
    `::warning::cache-seed.tar.gz is ${(tarSize / 1024 / 1024).toFixed(0)}MB` +
      `, over the ${SEED_WARN_MB}MB limit. Prerender fewer pages in getStaticPaths or ` +
      `generateStaticParams.`,
  )
}
