import {existsSync, mkdirSync, writeFileSync} from 'node:fs'
import {dirname, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {defineConfig, type Plugin} from 'vite'
import react from '@vitejs/plugin-react'

// The Go side embeds all:frontend/dist, and a clean checkout has nothing there
// to embed because the build output is ignored. A zero-byte placeholder is
// tracked so the embed has a match and `go build` works before anyone runs the
// frontend build.
//
// Vite empties the output directory before writing, so every `npm run build`
// deletes that placeholder, leaving a working tree where a `git add -A` re-breaks
// the embed. It has been restored by hand three times. Re-creating it here means
// the build cannot leave the tree in that state, rather than relying on everyone
// remembering not to commit the deletion.
function embedPlaceholder(): Plugin {
  return {
    name: 'embed-placeholder',
    // closeBundle runs after the files are written, which is the only point at
    // which they will not be deleted again this build.
    closeBundle() {
      const outDir = resolve(dirname(fileURLToPath(import.meta.url)), 'dist')
      const placeholder = resolve(outDir, 'DIST.placeholder')
      if (existsSync(placeholder)) {
        return
      }
      mkdirSync(outDir, {recursive: true})
      writeFileSync(placeholder, '')
      this.warn('re-created the Go embed placeholder that the build emptied')
    },
  }
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), embedPlaceholder()]
})
