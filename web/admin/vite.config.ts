import { defineConfig, loadEnv } from 'vite'
import preact from '@preact/preset-vite'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const adminApiTarget = env.VITE_ADMIN_API_TARGET || env.ADMIN_API_TARGET || 'http://127.0.0.1:18081'

  return {
    plugins: [preact()],
    base: '/admin/',
    build: {
      target: 'es2020',
      outDir: 'dist',
      emptyOutDir: true,
      cssMinify: true,
      rollupOptions: {
        output: {
          manualChunks: (id) => {
            if (id.includes('preact')) return 'preact'
            return 'vendor'
          },
        },
      },
    },
    server: {
      proxy: {
        '/api': {
          target: adminApiTarget,
        },
      },
    },
  }
})
