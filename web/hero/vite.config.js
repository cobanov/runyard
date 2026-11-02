import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The website's hero animation (Stageshow kit, React), built into www/hero/hero.js and hero.css.
// The dev page (index.html here) borrows the site's fonts from www through publicDir.
export default defineConfig({
  root: import.meta.dirname,
  publicDir: '../../www',
  plugins: [react()],
  build: {
    outDir: '../../www/hero',
    emptyOutDir: true,
    copyPublicDir: false,
    rollupOptions: {
      input: resolve(import.meta.dirname, 'main.tsx'),
      output: { entryFileNames: 'hero.js', assetFileNames: 'hero[extname]' },
    },
  },
})
