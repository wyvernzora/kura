import type { Meta, StoryObj } from '@storybook/react';

import type { Status } from '@/lib/status';

import { StatusChip } from './status-chip';

const meta: Meta<typeof StatusChip> = {
  title: 'Primitives/StatusChip',
  component: StatusChip,
  parameters: {
    layout: 'centered',
  },
};

export default meta;

type Story = StoryObj<typeof StatusChip>;

const ALL_STATUSES: Status[] = ['complete', 'incomplete', 'airing', 'untracked', 'error'];

export const SingleStatuses: Story = {
  render: () => (
    <div className="flex flex-wrap gap-2">
      {ALL_STATUSES.map((s) => (
        <StatusChip key={s} status={s} />
      ))}
    </div>
  ),
};

export const Compound: Story = {
  render: () => (
    <div className="flex flex-wrap gap-2">
      <StatusChip status={['airing', 'incomplete']} />
      <StatusChip status={['error', 'incomplete']} />
      <StatusChip status={['airing', 'untracked']} />
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="flex items-center gap-3">
      <StatusChip status="airing" size="sm" />
      <StatusChip status="airing" size="md" />
    </div>
  ),
};
