import type { Meta, StoryObj } from '@storybook/react';

import { Logo } from './Logo';

const meta: Meta<typeof Logo> = {
  title: 'Chrome/Logo',
  component: Logo,
  parameters: { layout: 'centered' },
};

export default meta;

type Story = StoryObj<typeof Logo>;

// Storybook renders Logo outside any router context, so the
// interactive variant (Link wrapper + useRouterState) would explode
// at mount time. The static variant is what GearMenu uses; render
// that here. The interactive variant gets exercised end-to-end by
// the TopBar stories (which set up a router) and by the running
// app.
export const Default: Story = {
  args: { interactive: false },
};
