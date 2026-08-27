import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    cssTarget: 'safari12',
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://10.0.0.6:8080',
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq, req) => {
            const targetOrigin = 'http://10.0.0.6:8080'
            if (req.headers.origin) proxyReq.setHeader('origin', targetOrigin)
            if (req.headers.referer) {
              const referer = req.headers.referer.replace(/^https?:\/\/[^\/]+/, targetOrigin)
              proxyReq.setHeader('referer', referer)
            }
          })
        },
      },
    },
  },
})
