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

/**
 * Resting variants — no staged change pending. `staged` shares the
 * missing color (no file on disk yet); `staged_replacement` shares
 * the present color (file present, swap queued). The amber pulse
 * never lights up here — see WithStagedHalo for that overlay.
 */
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

/**
 * Same set with the staged-change halo on. Use this story to confirm
 * the amber pulse is visible against every base color and that the
 * dot itself stays at 10 px (the halo lives in the box-shadow ring,
 * not the layout box).
 */
export const WithStagedHalo: Story = {
  render: () => (
    <div className="flex items-center gap-6">
      {STATUSES.map((s) => (
        <div key={s} className="flex items-center gap-2 text-sm text-ink">
          <StatusDot status={s} staged />
          <span>{s} + staged</span>
        </div>
      ))}
    </div>
  ),
};

/**
 * Side-by-side comparison: resting vs. staged-halo. Each row pairs
 * the same base status so the eye picks up the amber overlay
 * directly.
 */
export const RestingVsStaged: Story = {
  render: () => (
    <div className="grid grid-cols-[auto_auto_auto] items-center gap-x-4 gap-y-3 text-sm text-ink">
      {STATUSES.map((s) => (
        <div key={s} className="contents">
          <span className="font-mono text-[11px] text-muted">{s}</span>
          <StatusDot status={s} />
          <StatusDot status={s} staged />
        </div>
      ))}
    </div>
  ),
};
