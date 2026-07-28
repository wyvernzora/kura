import type { Meta, StoryObj } from '@storybook/react';

import { SeriesDetailSkeleton } from './SeriesDetailSkeleton';

const meta: Meta<typeof SeriesDetailSkeleton> = {
  title: 'Series/SeriesDetailSkeleton',
  component: SeriesDetailSkeleton,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof SeriesDetailSkeleton>;

export const Default: Story = {
  render: () => (
    <div className="min-h-dvh bg-paper">
      <SeriesDetailSkeleton />
    </div>
  ),
};
