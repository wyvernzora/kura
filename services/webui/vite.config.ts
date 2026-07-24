import { existsSync } from 'node:fs';
import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { TanStackRouterVite } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Polling watcher is mandatory inside Docker on macOS / Windows hosts:
// VirtioFS / gRPC-FUSE don't propagate inotify events into the Linux
// container, so the default watcher misses every host edit. Detect
// the container via `/.dockerenv` (present in every Docker image) so
// we don't depend on env wiring through the Makefile + entrypoint.
// `KURA_DISABLE_POLLING=1` lets a Linux-host devloop opt out if the
// 300 ms tick ever shows up in CPU profiles. Storybook reads the
// same flag via `.storybook/main.ts`.
const inDocker = existsSync('/.dockerenv');
const usePolling =
  process.env.KURA_DISABLE_POLLING !== '1' &&
  (inDocker || process.env.CHOKIDAR_USEPOLLING === '1' || process.env.VITE_USE_POLLING === '1');

// Vite proxy keeps browser requests same-origin in dev (browser hits :5173,
// Vite forwards /api to :8080). No CORS dance, no token rewrite — bearer
// auth flows through unchanged when token mode is enabled, and works
// transparently when auth is disabled in the library-manager config.
export default defineConfig({
  plugins: [
    TanStackRouterVite({
      target: 'react',
      autoCodeSplitting: true,
      routesDirectory: 'src/routes',
      generatedRouteTree: 'src/routeTree.gen.ts',
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    host: true,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
    // See top of file: `usePolling` is true inside Docker; native fs
    // events on the host. 300 ms tick is comfortably under "feels
    // instant" while staying off the CPU profile.
    watch: usePolling
      ? {
          usePolling: true,
          interval: Number(process.env.CHOKIDAR_INTERVAL) || 300,
        }
      : undefined,
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
});
