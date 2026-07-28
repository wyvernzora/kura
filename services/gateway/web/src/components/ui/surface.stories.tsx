import type { Meta, StoryObj } from '@storybook/react';

import { Surface } from './surface';

const meta: Meta<typeof Surface> = {
  title: 'Primitives/Surface',
  component: Surface,
  parameters: {
    layout: 'centered',
  },
};

export default meta;

type Story = StoryObj<typeof Surface>;

export const Default: Story = {
  args: {
    className: 'p-6 max-w-sm',
    children: (
      <div className="space-y-1">
        <div className="text-sm font-semibold tracking-tight">Surface</div>
        <div className="text-sm text-muted">
          Flat colored region. No elevation; quietly groups its children.
        </div>
      </div>
    ),
  },
};

export const Bordered: Story = {
  args: {
    bordered: true,
    className: 'p-6 max-w-sm',
    children: (
      <div className="space-y-1">
        <div className="text-sm font-semibold tracking-tight">Bordered surface</div>
        <div className="text-sm text-muted">
          Subtle outline reads on the page background without claiming elevation.
        </div>
      </div>
    ),
  },
};
