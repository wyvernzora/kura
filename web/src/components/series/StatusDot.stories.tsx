import type { Meta, StoryObj } from '@storybook/react';

import type { EpisodeStatus } from '@/api/types';

import { StatusDot } from './StatusDot';

const meta: Meta<typeof StatusDot> = {
  title: 'Series/StatusDot',
  component: StatusDot,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof StatusDot>;

const STATUSES: EpisodeStatus[] = ['present', 'staged', 'staged_replacement', 'missing', 'pending'];

export const All: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      {STATUSES.map((s) => (
        <div key={s} className="flex items-center gap-2 text-sm text-ink">
          <StatusDot status={s} />
          <span>{s}</span>
        </div>
      ))}
    </div>
  ),
};
