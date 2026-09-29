/**
 * Platform build step for Next.js and GraphCommerce projects: wire the shared
 * ISR cache handler into the project's Next config AT BUILD TIME, so a
 * project carries no cache code and no cache config of its own.
 *
 * Runs in the Dockerfile builder stage, in the project root, before
 * `next build`. It renames the project's config file to
 * `next.config.project.<ext>` and writes a wrapper under the original config
 * name that imports it and adds three settings:
 *
 *   cacheHandler        -> <root>/deployyy-cache-handler.mjs (the platform
 *                          handler, copied next to the config by the
 *                          Dockerfile). Next traces the file into the
 *                          standalone output.
 *   cacheMaxMemorySize  -> 0. Every replica must read the shared store: an
 *                          in-memory layer keeps a stale copy per pod after a
 *                          revalidation on another pod (docs/GC-CACHE.md in
 *                          the operator repo).
 *   output              -> 'standalone' when the project sets no output. The
 *                          runtime image runs `node server.js`.
 *   generateBuildId     -> the commit sha (DEPLOYYY_BUILD_ID), when the
 *                          project sets none. The cache seed and the runtime
 *                          image are two build targets; a fixed build id
 *                          guarantees the seed is keyed on the build id the
 *                          server reads, also when the builder stage runs
 *                          twice.
 *
 * Why a wrapper and not NEXT_CACHE_HANDLER_PATH: Next 14 has no such
 * variable, and no Next version has one for cacheMaxMemorySize.
 *
 * A project that sets its own `cacheHandler` keeps it (and its memory
 * setting): the build prints a warning and does not override it. A project
 * that sets `output: 'export'` has no server to run, so the build stops.
 *
 * The wrapper accepts every config shape Next accepts: an object, a promise,
 * or a function of (phase, { defaultConfig }).
 */
import fs from 'node:fs'
import path from 'node:path'

const root = process.cwd()
const HANDLER = 'deployyy-cache-handler.mjs'

if (!fs.existsSync(path.join(root, HANDLER))) {
  console.error(`FATAL: ${HANDLER} is missing in ${root}; the Dockerfile must copy it first.`)
  process.exit(1)
}

// Next's own lookup order (next/dist/shared/lib/constants CONFIG_FILES).
const candidates = ['next.config.js', 'next.config.mjs', 'next.config.ts', 'next.config.mts']
const found = candidates.filter((f) => fs.existsSync(path.join(root, f)))
if (found.length > 1) {
  console.log(`::warning::more than one Next config file (${found.join(', ')}); Next uses ${found[0]}, so does the platform.`)
}
const original = found[0]

const ext = original ? path.extname(original) : null
const isTs = ext === '.ts' || ext === '.mts'
const projectFile = original ? `next.config.project${ext}` : null
if (original) {
  fs.renameSync(path.join(root, original), path.join(root, projectFile))
  // Only Next's first match counts; the others are left as they were.
}

const wrapperName = isTs ? original : 'next.config.mjs'
if (!isTs) {
  for (const f of ['next.config.js', 'next.config.mjs']) {
    if (f !== wrapperName && fs.existsSync(path.join(root, f))) {
      console.error(`FATAL: ${f} still exists next to the generated ${wrapperName}; Next would read ${f}.`)
      process.exit(1)
    }
  }
}

// TypeScript configs are loaded by Next itself (SWC, or Node's own type
// stripping on Next 16), so the wrapper is TS too. The import names the file
// with its extension, which native ESM needs; `@ts-nocheck` keeps the
// project's type check from rejecting that. JS configs get an ESM wrapper; an
// ESM import of a CommonJS config yields module.exports as the default export.
const importLine = projectFile
  ? `import * as project from './${projectFile}'`
  : `const project = { default: {} }`

const body = `${isTs ? '// @ts-nocheck\n' : ''}// GENERATED AT BUILD TIME by the deployyy platform build (inject-next-config.mjs).
// Your own config is in ${projectFile ?? '(none)'}; edit that file, not this one.
${importLine}
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const handler = path.join(path.dirname(fileURLToPath(import.meta.url)), '${HANDLER}')
const loaded${isTs ? ': any' : ''} = (project${isTs ? ' as any' : ''}).default ?? project

export default async function deployyyConfig(phase${isTs ? ': string' : ''}, ctx${isTs ? ': any' : ''}) {
  const base = typeof loaded === 'function' ? await loaded(phase, ctx) : await loaded
  const config = { ...(base ?? {}) }
  if (config.output === 'export') {
    throw new Error("deployyy: output: 'export' builds static files only; the platform runs a Next.js server. Remove output: 'export'.")
  }
  if (!config.output) config.output = 'standalone'
  if (!config.generateBuildId && process.env.DEPLOYYY_BUILD_ID) {
    const buildId = process.env.DEPLOYYY_BUILD_ID
    config.generateBuildId = () => buildId
  }
  if (config.cacheHandler) {
    console.warn('deployyy: the project sets its own cacheHandler; the platform cache handler is NOT used.')
  } else {
    config.cacheHandler = handler
    config.cacheMaxMemorySize = 0
  }
  return config
}
`

fs.writeFileSync(path.join(root, wrapperName), body)
console.log(
  `next config: ${original ? `${original} -> ${projectFile}` : 'no project config'}; ` +
    `wrapper ${wrapperName} sets cacheHandler=${HANDLER}, cacheMaxMemorySize=0, output=standalone (unless set)`,
)
