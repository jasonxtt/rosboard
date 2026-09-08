import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const devProxy = process.env.ROSBOARD_DEV_PROXY || 'http://127.0.0.1:8090'

export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    cssTarget: 'safari12',
    manifest: true,
  },
  server: {
    proxy: {
      '/api': {
        target: devProxy,
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq, req) => {
            const targetOrigin = devProxy
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
