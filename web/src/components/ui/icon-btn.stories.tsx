import type { Meta, StoryObj } from '@storybook/react';
import { Bell, Search, Settings } from 'lucide-react';

import { IconBtn } from './icon-btn';

const meta: Meta<typeof IconBtn> = {
  title: 'Primitives/IconBtn',
  component: IconBtn,
  parameters: {
    layout: 'centered',
  },
  args: {
    'aria-label': 'Settings',
  },
  argTypes: {
    active: { control: 'boolean' },
    disabled: { control: 'boolean' },
    badge: { control: 'text' },
  },
};

export default meta;

type Story = StoryObj<typeof IconBtn>;

export const Default: Story = {
  render: (args) => (
    <IconBtn {...args}>
      <Settings className="h-[15px] w-[15px]" />
    </IconBtn>
  ),
};

export const Active: Story = {
  args: { active: true },
  render: (args) => (
    <IconBtn {...args}>
      <Settings className="h-[15px] w-[15px]" />
    </IconBtn>
  ),
};

export const WithBadge: Story = {
  args: { badge: 3, 'aria-label': 'Notifications' },
  render: (args) => (
    <IconBtn {...args}>
      <Bell className="h-[15px] w-[15px]" />
    </IconBtn>
  ),
};

export const Disabled: Story = {
  args: { disabled: true },
  render: (args) => (
    <IconBtn {...args}>
      <Settings className="h-[15px] w-[15px]" />
    </IconBtn>
  ),
};

export const Hovered: Story = {
  parameters: { pseudo: { hover: true } },
  render: () => (
    <IconBtn aria-label="Settings">
      <Settings className="h-[15px] w-[15px]" />
    </IconBtn>
  ),
};

export const Row: Story = {
  render: () => (
    <div className="flex items-center gap-1.5">
      <IconBtn aria-label="Search">
        <Search className="h-[15px] w-[15px]" />
      </IconBtn>
      <IconBtn aria-label="Notifications" badge={3}>
        <Bell className="h-[15px] w-[15px]" />
      </IconBtn>
      <IconBtn aria-label="Settings" active>
        <Settings className="h-[15px] w-[15px]" />
      </IconBtn>
    </div>
  ),
};
