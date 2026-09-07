import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const targetOrigin = process.env.ROSBOARD_DEV_PROXY || 'http://127.0.0.1:8080'

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
        target: targetOrigin,
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq, req) => {
            if (req.headers.origin) proxyReq.setHeader('origin', targetOrigin)
            if (req.headers.referer) {
              const referer = req.headers.referer.replace(/^https?:\/\/[^/]+/, targetOrigin)
              proxyReq.setHeader('referer', referer)
            }
          })
        },
      },
    },
  },
})
