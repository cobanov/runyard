import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The demo at runyard.cobanov.dev/demo/: the same app, built with mock.js in front, into the website's folder.
export default defineConfig({
  root: import.meta.dirname,
  base: '/demo/',
  plugins: [svelte()],
  build: { outDir: '../../www/demo', emptyOutDir: true },
  server: { fs: { allow: ['..'] } },
})
