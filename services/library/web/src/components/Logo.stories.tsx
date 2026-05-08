import type { Meta, StoryObj } from '@storybook/react';

import { Logo } from './Logo';

const meta: Meta<typeof Logo> = {
  title: 'Chrome/Logo',
  component: Logo,
  parameters: { layout: 'centered' },
};

export default meta;

type Story = StoryObj<typeof Logo>;

export const Default: Story = {};
