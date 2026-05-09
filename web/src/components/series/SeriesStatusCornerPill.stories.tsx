import type { Meta, StoryObj } from '@storybook/react';

import type { Status } from '@/lib/status';

import { SeriesStatusCornerPill } from './SeriesStatusCornerPill';

const meta: Meta<typeof SeriesStatusCornerPill> = {
  title: 'Series/SeriesStatusCornerPill',
  component: SeriesStatusCornerPill,
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div className="relative h-[280px] w-[200px] overflow-hidden rounded-[12px] bg-line-soft shadow-card">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof SeriesStatusCornerPill>;

const ALL_STATUSES: Status[] = ['airing', 'complete', 'incomplete', 'untracked', 'error'];

export const Airing: Story = { args: { status: 'airing' } };
export const Complete: Story = { args: { status: 'complete' } };
export const Incomplete: Story = { args: { status: 'incomplete' } };
export const Untracked: Story = { args: { status: 'untracked' } };
export const ErrorStatus: Story = { args: { status: 'error' } };

/** Side-by-side gallery for visual sweep. */
export const Gallery: Story = {
  decorators: [],
  render: () => (
    <div className="flex flex-wrap gap-3">
      {ALL_STATUSES.map((status) => (
        <div
          key={status}
          className="relative h-[280px] w-[200px] overflow-hidden rounded-[12px] bg-line-soft shadow-card"
        >
          <SeriesStatusCornerPill status={status} />
        </div>
      ))}
    </div>
  ),
};
