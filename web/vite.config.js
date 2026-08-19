const path = require("path")
const { defineConfig } = require("vite")

module.exports = defineConfig({
  build: {
    outDir: path.resolve(__dirname, "../internal/web/dist"),
    emptyOutDir: true,
  },
})
