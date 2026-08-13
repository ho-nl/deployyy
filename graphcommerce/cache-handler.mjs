/**
 * Platform ISR cache handler for GraphCommerce projects.
 *
 * This file is owned by the platform (ho-nl/deployyy). The build workflow
 * downloads it into the project build context as `cache-handler.mjs`. The
 * Dockerfile sets NEXT_CACHE_HANDLER_PATH=/app/cache-handler.mjs during
 * `next build` — Next's own extension point (the default value of the
 * `cacheHandler` config option). Projects carry NO cache code of their own.
 *
 * Runtime configuration is ONE env variable:
 *   CACHE_DIR  — the shared cache directory (the per-env NFS volume, mounted
 *                at /cache and pinned actimeo=1 by the operator). When unset
 *                (local dev, tests) it falls back to a pod-local directory
 *                and behaves like a private cache.
 *
 * Deliberately minimal — each removal is covered elsewhere in the platform:
 *   - no compression: the volume sits on btrfs with transparent zstd
 *     (measured 200MB -> 10.3MB), gzip here would only cost CPU;
 *   - no image caching: Bunny caches /_next/image variants at the edge and
 *     perma-cache persists them, the origin only optimizes on an edge miss;
 *   - no queue/coordination: with actimeo=1 a write is visible on every
 *     replica within ~1s, and Next's stale-while-revalidate semantics
 *     tolerate the last bit of overlap.
 *
 * Page entries are namespaced by build id; a marker per build lets the sweep
 * remove entries of superseded builds one hour after a newer build appeared
 * (old and new pods overlap during a rolling deploy). Fetch entries and tag
 * stamps are shared across builds on purpose.
 */
import fs from 'node:fs/promises'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { createHash } from 'node:crypto'

const CACHE_DIR = process.env.CACHE_DIR || path.join(process.cwd(), '.next', 'cache', 'shared')
const BUILD_GRACE_MS = 60 * 60 * 1000
const EVICTION_INTERVAL_MS = 10 * 60 * 1000

const dirs = {
  builds: () => path.join(CACHE_DIR, '_builds'),
  tags: () => path.join(CACHE_DIR, '_tags'),
  data: (buildId) => path.join(CACHE_DIR, 'data', buildId),
  fetch: () => path.join(CACHE_DIR, 'fetch'),
}

function md5(str) {
  return createHash('md5').update(str).digest('hex')
}

function replacer(_key, value) {
  if (Buffer.isBuffer(value)) return { __b: value.toString('base64') }
  if (value instanceof Uint8Array) return { __b: Buffer.from(value).toString('base64') }
  if (value instanceof Map) return { __m: Array.from(value.entries()) }
  return value
}

function reviver(_key, value) {
  if (value && typeof value === 'object') {
    if ('__b' in value) return Buffer.from(value.__b, 'base64')
    if ('__m' in value) return new Map(value.__m)
  }
  return value
}

async function atomicWrite(filePath, data) {
  await fs.mkdir(path.dirname(filePath), { recursive: true })
  const tmp = `${filePath}.${process.pid}.${Date.now()}.tmp`
  await fs.writeFile(tmp, data)
  await fs.rename(tmp, filePath)
}

async function safeReadFile(filePath, encoding) {
  try {
    return await fs.readFile(filePath, encoding)
  } catch (err) {
    if (err.code === 'ENOENT') return null
    throw err
  }
}

let initialized = false
let evictionTimer = null

export default class CacheHandler {
  static buildId = 'default'
  static revalidatedTags = []

  constructor(ctx) {
    if (ctx?.revalidatedTags) {
      CacheHandler.revalidatedTags = ctx.revalidatedTags
    }

    if (!initialized) {
      initialized = true

      try {
        const distDir = ctx?.serverDistDir || path.join(process.cwd(), '.next', 'server')
        CacheHandler.buildId = readFileSync(path.join(distDir, '..', 'BUILD_ID'), 'utf8').trim()
      } catch {}

      atomicWrite(path.join(dirs.builds(), CacheHandler.buildId), Date.now().toString()).catch(
        () => {},
      )

      if (!evictionTimer) {
        evictionTimer = setInterval(() => this.#evictOldBuilds().catch(() => {}), EVICTION_INTERVAL_MS)
        evictionTimer.unref()
      }
    }
  }

  async #evictOldBuilds() {
    let entries
    try {
      entries = await fs.readdir(dirs.builds())
    } catch {
      return
    }

    const currentMarker = await safeReadFile(path.join(dirs.builds(), CacheHandler.buildId), 'utf8')
    if (!currentMarker) return
    const currentTs = parseInt(currentMarker, 10)

    for (const buildId of entries) {
      if (buildId === CacheHandler.buildId) continue
      try {
        const content = await fs.readFile(path.join(dirs.builds(), buildId), 'utf8')
        const ts = parseInt(content, 10)
        if (ts > currentTs) continue
        if (Date.now() - ts < BUILD_GRACE_MS) continue
        await fs.rm(path.join(CACHE_DIR, 'data', buildId), { recursive: true, force: true })
        await fs.rm(path.join(dirs.builds(), buildId), { force: true })
        // The seed Job's once-per-build marker goes with the build.
        await fs.rm(path.join(CACHE_DIR, '_seeded', buildId), { force: true })
      } catch {}
    }
  }

  // ---------------------------------------------------------------------------
  // CacheHandler interface
  // ---------------------------------------------------------------------------

  async get(key, ctx) {
    try {
      if (ctx?.kind === 'IMAGE') return null // Bunny owns image caching at the edge
      if (ctx?.kind === 'FETCH') return await this.#getFetch(key, ctx)
      return await this.#getPage(key)
    } catch (err) {
      if (err.code !== 'ENOENT') console.error('[cache-handler] get error:', err.message)
      return null
    }
  }

  async set(key, data, ctx) {
    if (!data) return
    try {
      if (data.kind === 'IMAGE') return // Bunny owns image caching at the edge
      if (data.kind === 'FETCH') return await this.#setFetch(key, data, ctx)
      return await this.#setPage(key, data)
    } catch (err) {
      console.error('[cache-handler] set error:', err.message)
    }
  }

  async revalidateTag(tags, _durations) {
    if (typeof tags === 'string') tags = [tags]
    const expired =
      _durations?.expire !== undefined ? Date.now() + _durations.expire * 1000 : Date.now()

    await Promise.all(
      tags.map((tag) => atomicWrite(path.join(dirs.tags(), md5(tag)), expired.toString())),
    )
  }

  resetRequestCache() {}

  // ---------------------------------------------------------------------------
  // Fetch cache (plain JSON, shared across builds)
  // ---------------------------------------------------------------------------

  async #getFetch(key, ctx) {
    const filePath = path.join(dirs.fetch(), `${md5(key)}.json`)
    const data = JSON.parse(await fs.readFile(filePath, 'utf8'), reviver)
    const { mtimeMs } = await fs.stat(filePath)

    const combinedTags = [...(data.tags || []), ...(ctx?.tags || []), ...(ctx?.softTags || [])]
    if (combinedTags.some((t) => CacheHandler.revalidatedTags.includes(t))) return null
    if (await this.#areTagsExpired(combinedTags, mtimeMs)) return null

    return { lastModified: mtimeMs, value: data }
  }

  async #setFetch(key, data, ctx) {
    const toStore = { ...data, tags: ctx?.tags || data.tags || [] }
    await atomicWrite(path.join(dirs.fetch(), `${md5(key)}.json`), JSON.stringify(toStore, replacer))
  }

  // ---------------------------------------------------------------------------
  // Page / route cache (plain JSON, namespaced by build id)
  // ---------------------------------------------------------------------------

  async #getPage(key) {
    const filePath = path.join(dirs.data(CacheHandler.buildId), `${md5(key)}.json`)
    const data = JSON.parse(await fs.readFile(filePath, 'utf8'), reviver)
    const { mtimeMs } = await fs.stat(filePath)

    const tagsHeader = data.headers?.['x-next-cache-tags']
    if (typeof tagsHeader === 'string') {
      const cacheTags = tagsHeader.split(',').filter(Boolean)
      if (cacheTags.some((t) => CacheHandler.revalidatedTags.includes(t))) return null
      if (await this.#areTagsExpired(cacheTags, mtimeMs)) return null
    }

    return { lastModified: mtimeMs, value: data }
  }

  async #setPage(key, data) {
    await atomicWrite(
      path.join(dirs.data(CacheHandler.buildId), `${md5(key)}.json`),
      JSON.stringify(data, replacer),
    )
  }

  // ---------------------------------------------------------------------------
  // Tag expiry
  // ---------------------------------------------------------------------------

  async #areTagsExpired(tags, lastModified) {
    if (!tags.length) return false

    const checks = tags.map(async (tag) => {
      const content = await safeReadFile(path.join(dirs.tags(), md5(tag)), 'utf8')
      if (!content) return false
      return parseInt(content, 10) >= lastModified
    })

    return (await Promise.all(checks)).some(Boolean)
  }
}
