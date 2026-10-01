import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { analyzer } from 'vite-bundle-analyzer'
import { buildSync } from 'esbuild'
import path from 'path'
import { fileURLToPath } from 'url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

function serviceWorkerPlugin(): Plugin {
  const getCompiledSW = () => {
    const result = buildSync({
      entryPoints: [path.resolve(__dirname, 'src/sw/index.ts')],
      bundle: true,
      format: 'iife',
      target: 'es2020',
      write: false,
    })
    return result.outputFiles[0].text
  }

  return {
    name: 'service-worker-plugin',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const pathname = req.url ? req.url.split('?')[0] : ''
        if (pathname === '/sw.js') {
          res.setHeader('Content-Type', 'application/javascript; charset=utf-8')
          res.end(getCompiledSW())
          return
        }
        next()
      })
    },
    generateBundle() {
      this.emitFile({
        type: 'asset',
        fileName: 'sw.js',
        source: getCompiledSW(),
      })
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    analyzer({
      analyzerMode: 'static',
      openAnalyzer: false,
    }),
    serviceWorkerPlugin(),
  ],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/setupTests.tsx',
  },
})

