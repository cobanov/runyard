import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The App Store screenshots (store/render.sh); the site's fonts come from www through publicDir.
export default defineConfig({
  root: import.meta.dirname,
  publicDir: '../../www',
  plugins: [react()],
})
