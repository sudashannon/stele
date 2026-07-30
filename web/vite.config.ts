import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { configDefaults } from 'vitest/config'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Keep large libraries out of the entry chunk. Cytoscape is imported
        // synchronously by WikiGraph, while App's lazy WikiGraph boundary
        // defers that graph chunk. DiagramBlock dynamically imports Mermaid
        // (and its KaTeX dependency), so those renderers load only when needed;
        // manualChunks names vendor boundaries but does not create them.
        manualChunks: {
          cytoscape: ['cytoscape'],
          react: ['react', 'react-dom'],
          markdown: ['react-markdown', 'remark-gfm', 'rehype-slug', 'github-slugger'],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8989', changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    exclude: [...configDefaults.exclude, 'tests/**'],
  },
})
