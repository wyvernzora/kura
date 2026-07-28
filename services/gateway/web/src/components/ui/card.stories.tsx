import type { Meta, StoryObj } from '@storybook/react';

import { Card } from './card';

const meta: Meta<typeof Card> = {
  title: 'Primitives/Card',
  component: Card,
  parameters: {
    layout: 'centered',
  },
};

export default meta;

type Story = StoryObj<typeof Card>;

export const Default: Story = {
  args: {
    className: 'p-6 max-w-sm',
    children: (
      <div className="space-y-1">
        <div className="text-sm font-semibold tracking-tight">Card</div>
        <div className="text-sm text-muted">
          Levitating surface with two-tier shadow. Default elevation, no hover behavior.
        </div>
      </div>
    ),
  },
};

export const Interactive: Story = {
  args: {
    interactive: true,
    className: 'p-6 max-w-sm',
    children: (
      <div className="space-y-1">
        <div className="text-sm font-semibold tracking-tight">Interactive card</div>
        <div className="text-sm text-muted">Hover lifts the card and amplifies the shadow.</div>
      </div>
    ),
  },
};

export const Stack: Story = {
  render: () => (
    <div className="grid w-[640px] gap-4 sm:grid-cols-2">
      <Card className="p-5">
        <div className="text-sm font-semibold">First</div>
        <div className="mt-1 text-sm text-muted">Static card.</div>
      </Card>
      <Card interactive className="p-5">
        <div className="text-sm font-semibold">Second</div>
        <div className="mt-1 text-sm text-muted">Interactive card — hover me.</div>
      </Card>
      <Card className="col-span-full p-5">
        <div className="text-sm font-semibold">Wide</div>
        <div className="mt-1 text-sm text-muted">
          Cards compose into grids; padding is the caller&apos;s responsibility.
        </div>
      </Card>
    </div>
  ),
};
