import type { Meta, StoryObj } from '@storybook/react';

import { ErrorScreen } from './ErrorScreen';

const meta: Meta<typeof ErrorScreen> = {
  title: 'Auth/ErrorScreen',
  component: ErrorScreen,
  parameters: {
    layout: 'fullscreen',
  },
  args: {
    onRetry: () => {},
  },
};

export default meta;

type Story = StoryObj<typeof ErrorScreen>;

export const Network: Story = { args: { reason: 'network' } };

export const FiveXX: Story = { args: { reason: '503' } };
