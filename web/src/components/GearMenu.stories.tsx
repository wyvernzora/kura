import type { Meta, StoryObj } from '@storybook/react';

import { GearMenu } from './GearMenu';

const meta: Meta<typeof GearMenu> = {
  title: 'Chrome/GearMenu',
  component: GearMenu,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof GearMenu>;

export const Default: Story = {};
