import { defineConfig } from 'vite'
import preact from '@preact/preset-vite'

export default defineConfig({
  plugins: [preact()],
  base: '/admin/',
  build: {
    target: 'es2020',
    outDir: 'dist',
    emptyOutDir: true,
    cssMinify: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['preact', 'preact/hooks', 'preact/compat'],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:18080',
    },
  },
})
