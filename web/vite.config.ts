import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

const here = (p: string) => fileURLToPath(new URL(p, import.meta.url))

// Two pages: the app, and the note editor (the same Milkdown editor the Pocket
// Homelab app ships), which runs in a frame of the app's page.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      // The editor brings its own short list of languages; the full catalogue is not shipped.
      { find: /^@codemirror\/language-data$/, replacement: here('./src/editor/stubs/language-data.js') },
      // Crepe's own LaTeX feature is off; formulas load KaTeX themselves (math.js).
      { find: /^katex$/, replacement: here('./src/editor/stubs/katex.js') },
    ],
  },
  build: {
    chunkSizeWarningLimit: 4000,
    rollupOptions: {
      input: { main: here('./index.html'), editor: here('./editor.html') },
    },
  },
  server: {
    proxy: { '/api': 'http://localhost:8080', '/index.php': 'http://localhost:8080', '/remote.php': 'http://localhost:8080' },
  },
})
