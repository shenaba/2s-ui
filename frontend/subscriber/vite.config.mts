import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The subscriber dashboard is served from the subscription endpoint which is
// already at a deep path (/{subPath}/{clientName}), so all asset URLs must be
// relative to stay valid. base: '' keeps the emitted index.html referencing
// ./assets/... instead of an absolute /assets/... path.
export default defineConfig({
  base: '',
  plugins: [vue()],
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
})
