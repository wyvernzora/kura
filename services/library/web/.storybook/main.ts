import { existsSync } from 'node:fs';
import path from 'node:path';

import type { StorybookConfig } from '@storybook/react-vite';

// Same Docker-detection trick as `web/vite.config.ts` — VirtioFS /
// gRPC-FUSE bind-mounts on macOS / Windows hosts don't propagate
// inotify events into the container, so Storybook silently misses
// every story edit unless we flip Vite's watcher into polling mode.
const inDocker = existsSync('/.dockerenv');
const usePolling =
  process.env.KURA_DISABLE_POLLING !== '1' &&
  (inDocker ||
    process.env.CHOKIDAR_USEPOLLING === '1' ||
    process.env.VITE_USE_POLLING === '1');

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(ts|tsx|mdx)'],
  addons: [
    '@storybook/addon-a11y',
    'storybook-addon-pseudo-states',
  ],
  framework: {
    name: '@storybook/react-vite',
    options: {},
  },
  // Mirror the @ alias so stories resolve component imports the same
  // way the app does. The Vite config block also pulls in Tailwind v4
  // so tokens load before any story renders, and forces polling when
  // running inside the dev container.
  viteFinal: async (cfg) => {
    cfg.resolve = cfg.resolve ?? {};
    cfg.resolve.alias = {
      ...(cfg.resolve.alias ?? {}),
      '@': path.resolve(import.meta.dirname, '../src'),
    };
    const tailwindcss = (await import('@tailwindcss/vite')).default;
    cfg.plugins = [...(cfg.plugins ?? []), tailwindcss()];
    if (usePolling) {
      cfg.server = cfg.server ?? {};
      cfg.server.watch = {
        ...(cfg.server.watch ?? {}),
        usePolling: true,
        interval: Number(process.env.CHOKIDAR_INTERVAL) || 300,
      };
    }
    return cfg;
  },
};

export default config;
