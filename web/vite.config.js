import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The build lands inside the Go module so the server can go:embed it.
export default defineConfig({
  plugins: [svelte()],
  build: { outDir: '../server/webdist/dist', emptyOutDir: true },
  // RUNYARD_API=https://some-server npm run dev points the dev server at real data.
  server: { proxy: Object.fromEntries(['/api', '/media', '/mcp'].map((p) => [p, { target: process.env.RUNYARD_API || 'http://localhost:8080', changeOrigin: true }])) },
})
