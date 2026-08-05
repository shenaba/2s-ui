// Plugins
import vue from '@vitejs/plugin-vue'

// Utilities
import { defineConfig } from 'vite'
import { fileURLToPath, URL } from 'node:url'

// @fontsource emits every @font-face as `woff2, woff`. Anything that can run this
// bundle supports woff2, so the fallback is never requested — but it still lands in
// dist/, and from there into the binary via web.go's //go:embed.
function stripWoffFallback() {
  return {
    name: 'strip-woff-fallback',
    enforce: 'pre' as const,
    transform(code: string, id: string) {
      if (!id.includes('fontsource') || !id.includes('.css')) return null
      // Only ever drops a *fallback* (the comma is required), so a woff-only
      // @font-face — if @fontsource ever ships one — is left intact.
      const out = code.replace(/,\s*url\([^)]*\.woff\)\s*format\('woff'\)/g, '')
      if (out === code) {
        this.warn(`no woff fallback found in ${id}; @fontsource may have changed its CSS shape`)
        return null
      }
      return out
    },
  }
}

export default defineConfig({
  base: '',
  plugins: [
    vue(),
    stripWoffFallback(),
  ],
  build: {
    manifest: false,
    outDir: 'dist',
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      // web.go serves assets/ with max-age=31536000, so a file whose bytes changed
      // must never reuse a name a browser may still hold. Content hashes do that,
      // and more precisely than a per-build id: an unchanged chunk keeps its name
      // so the cache still hits across an upgrade, only changed ones are refetched.
      //
      // [name] stays in front so split chunks remain tellable apart, and the hash
      // is what makes that safe — views/Tls.vue and types/tls.ts both reduce to
      // "tls", and rollup otherwise disambiguates same-name chunks with a trailing
      // counter whose value moves with module order.
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        // [extname] carries the dot. Covers the CSS chunks and the @fontsource
        // woff2 files, which previously landed under their bare upstream names and
        // so were served with a year-long max-age they could never invalidate.
        // public/assets/ (the two favicons) bypasses this pipeline and keeps the
        // literal names index.html hardcodes.
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    }
  },
  define: { 'process.env': {} },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
    extensions: ['.js', '.json', '.jsx', '.mjs', '.ts', '.tsx', '.vue'],
  },
  server: {
    port: 3000,
    proxy: {
      '/app/api': {
        target: 'http://localhost:2095',
        changeOrigin: true,
      },
      '/app/ws': {
        target: 'http://localhost:2095',
        ws: true,
        changeOrigin: true,
        // coder/websocket's default CSWSH check wants Origin host == Host.
        // changeOrigin rewrites Host to :2095 but leaves the browser's :3000
        // Origin; align it (lowercase key — http-proxy merges over the
        // already-lowercased incoming headers).
        headers: { origin: 'http://localhost:2095' },
      },
    },
  }
})
