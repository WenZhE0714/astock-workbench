const path = require("path")
const { defineConfig } = require("vite")

module.exports = defineConfig({
  server: process.env.ASTOCK_WEB_API ? { cors: false, proxy: { "/api": { target: process.env.ASTOCK_WEB_API, changeOrigin: false } } } : undefined,
  build: {
    outDir: path.resolve(__dirname, "../internal/web/dist"),
    emptyOutDir: true,
  },
})
