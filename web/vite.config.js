const path = require("path")
const { defineConfig } = require("vite")

module.exports = defineConfig({
  server: process.env.ASTOCK_WEB_API ? { proxy: { "/api": process.env.ASTOCK_WEB_API } } : undefined,
  build: {
    outDir: path.resolve(__dirname, "../internal/web/dist"),
    emptyOutDir: true,
  },
})
