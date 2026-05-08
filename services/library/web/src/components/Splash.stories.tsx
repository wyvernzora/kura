import type { Meta, StoryObj } from '@storybook/react';

import { Splash } from './Splash';

const meta: Meta<typeof Splash> = {
  title: 'Auth/Splash',
  component: Splash,
  parameters: {
    layout: 'fullscreen',
  },
};

export default meta;

type Story = StoryObj<typeof Splash>;

export const Default: Story = {};

export const Custom: Story = {
  args: { message: 'Validating token…' },
};
