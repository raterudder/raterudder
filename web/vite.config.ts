import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { analyzer } from 'vite-bundle-analyzer'
import { buildSync } from 'esbuild'
import path from 'path'
import process from 'process'
import { fileURLToPath } from 'url'
import { PUBLIC_PAGES_SEO, renderSubpageHtml } from './src/utils/seo.ts'

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

function seoSubpagesPlugin(): Plugin {
  return {
    name: 'seo-subpages-plugin',
    enforce: 'post',
    generateBundle(_, bundle) {
      const indexAsset = bundle['index.html']
      if (!indexAsset || indexAsset.type !== 'asset' || typeof indexAsset.source !== 'string') {
        return
      }
      for (const [slug, seo] of Object.entries(PUBLIC_PAGES_SEO)) {
        if (slug === 'index') continue
        this.emitFile({
          type: 'asset',
          fileName: `${slug}.html`,
          source: renderSubpageHtml(indexAsset.source, seo),
        })
      }
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    ...(process.env.ANALYZE === 'true'
      ? [
          analyzer({
            analyzerMode: 'static',
            openAnalyzer: false,
          }),
        ]
      : []),
    serviceWorkerPlugin(),
    seoSubpagesPlugin(),
  ],
  build: {
    modulePreload: {
      resolveDependencies: (_filename, deps, context) => {
        if (context.hostType === 'html') {
          return deps.filter(
            (dep) => !dep.includes('vendor-charts') && !dep.includes('vendor-ui')
          )
        }
        return deps
      },
    },
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'vendor-react',
              test: /node_modules[\\/](react|react-dom|scheduler|use-sync-external-store|@base-ui|@floating-ui)[\\/]/,
              tags: ['$initial'],
              priority: 30,
            },
            {
              name: 'vendor-charts',
              test: /node_modules[\\/](recharts|d3-[^\\/]+|victory-vendor|@reduxjs[\\/]toolkit|redux|reselect|immer|decimal\.js-light|es-toolkit|eventemitter3|tiny-invariant|clsx)[\\/]/,
              priority: 20,
            },
            {
              name: 'vendor-ui',
              test: /node_modules[\\/](@base-ui|@floating-ui)[\\/]/,
              priority: 10,
            },
          ],
        },
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/setupTests.tsx',
    maxWorkers: 2,
  },
})
