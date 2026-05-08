import type { Meta, StoryObj } from '@storybook/react';

import { LoginScreenView } from './LoginScreen';

const meta: Meta<typeof LoginScreenView> = {
  title: 'Auth/LoginScreen',
  component: LoginScreenView,
  parameters: {
    layout: 'fullscreen',
  },
  args: {
    onSubmit: () => {},
  },
};

export default meta;

type Story = StoryObj<typeof LoginScreenView>;

export const Default: Story = {};

export const InvalidToken: Story = {
  args: { error: 'That token doesn’t match. Try again.' },
};

export const Unreachable: Story = {
  args: { error: 'Can’t reach kura. Check the server is running.' },
};

export const Submitting: Story = {
  args: { submitting: true },
};
