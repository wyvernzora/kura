import type { Meta, StoryObj } from '@storybook/react';

import { ResolutionChip, SourceChip } from './QualityChip';

const meta: Meta = {
  title: 'Series/QualityChip',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const Sources: Story = {
  render: () => (
    <div className="flex flex-wrap items-center gap-2">
      {['BluRay', 'Web-DL', 'WebRip', 'TV', 'TVRip', 'HDTV', 'DVDRip', 'Unknown'].map((s) => (
        <SourceChip key={s} source={s} />
      ))}
    </div>
  ),
};

export const Resolutions: Story = {
  render: () => (
    <div className="flex flex-wrap items-center gap-2">
      {['4K', '2160p', '1080p', '720p', '480p', '360p'].map((r) => (
        <ResolutionChip key={r} resolution={r} />
      ))}
    </div>
  ),
};
