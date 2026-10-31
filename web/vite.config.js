import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The build lands inside the Go module so the server can go:embed it.
export default defineConfig({
  plugins: [svelte()],
  build: { outDir: '../server/webdist/dist', emptyOutDir: true },
  server: { proxy: { '/api': 'http://localhost:8080', '/media': 'http://localhost:8080', '/mcp': 'http://localhost:8080' } },
})
