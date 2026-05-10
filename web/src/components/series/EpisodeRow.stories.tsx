import type { Meta, StoryObj } from '@storybook/react';
import {
  FIXTURE_EPISODE_MISSING,
  FIXTURE_EPISODE_PENDING,
  FIXTURE_EPISODE_PRESENT,
} from './_fixtures';
import { EpisodeRow } from './EpisodeRow';

const meta: Meta<typeof EpisodeRow> = {
  title: 'Series/EpisodeRow',
  component: EpisodeRow,
  parameters: { layout: 'padded' },
  decorators: [
    (Story) => (
      <div className="w-[640px] rounded-[12px] bg-surface shadow-card">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof EpisodeRow>;

export const Present: Story = {
  args: { episode: FIXTURE_EPISODE_PRESENT },
};

export const Missing: Story = {
  args: { episode: FIXTURE_EPISODE_MISSING },
};

export const Pending: Story = {
  args: { episode: FIXTURE_EPISODE_PENDING },
};

export const Stack: Story = {
  render: () => (
    <>
      <EpisodeRow episode={FIXTURE_EPISODE_PRESENT} />
      <EpisodeRow episode={FIXTURE_EPISODE_MISSING} />
      <EpisodeRow episode={FIXTURE_EPISODE_PENDING} />
    </>
  ),
};
