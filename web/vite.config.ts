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
        // 默认代理到本机实例，避免开发时误触生产数据；可用 ROSBOARD_DEV_PROXY 覆盖
        target: process.env.ROSBOARD_DEV_PROXY || 'http://127.0.0.1:8080',
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq, req) => {
            const targetOrigin = process.env.ROSBOARD_DEV_PROXY || 'http://127.0.0.1:8080'
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
