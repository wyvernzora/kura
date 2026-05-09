import type { Meta, StoryObj } from '@storybook/react';

import { withStoryProviders } from '@/components/_storyProviders';

import { SeriesPosterCard } from './SeriesPosterCard';
import { FIXTURE_SHOW_AIRING, FIXTURE_SHOW_COMPLETE_SINGLE } from './_fixtures';

const meta: Meta<typeof SeriesPosterCard> = {
  title: 'Series/SeriesPosterCard',
  component: SeriesPosterCard,
  parameters: { layout: 'centered' },
  decorators: [
    // SeriesPosterCard mounts ScanButton → useScanJob → useQueryClient.
    // Without a provider the hook throws on mount.
    withStoryProviders,
    (Story) => (
      <div className="w-[300px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof SeriesPosterCard>;

export const AiringWithPoster: Story = {
  args: { show: FIXTURE_SHOW_AIRING },
};

export const Complete: Story = {
  args: { show: FIXTURE_SHOW_COMPLETE_SINGLE },
};
