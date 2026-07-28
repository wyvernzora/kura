import type { Preview } from '@storybook/react';

import '../src/styles/globals.css';
import './fetchMock';

const preview: Preview = {
  parameters: {
    controls: { expanded: true },
    backgrounds: { disable: true },
    layout: 'fullscreen',
  },
  globalTypes: {
    theme: {
      name: 'Theme',
      defaultValue: 'paper',
      toolbar: {
        icon: 'paintbrush',
        items: [
          { value: 'paper', title: 'Paper (light)' },
          { value: 'dark', title: 'Dark' },
        ],
        dynamicTitle: true,
      },
    },
  },
  decorators: [
    (Story, ctx) => {
      const theme = (ctx.globals.theme as string) ?? 'paper';
      document.documentElement.dataset.kTheme = theme;
      return Story();
    },
  ],
};

export default preview;
