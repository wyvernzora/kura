import type { Meta, StoryObj } from '@storybook/react';

import { SeasonPanel } from './SeasonPanel';
import { FIXTURE_SEASON_AIRING, FIXTURE_SEASON_SPECIALS } from './_fixtures';

const meta: Meta<typeof SeasonPanel> = {
  title: 'Series/SeasonPanel',
  component: SeasonPanel,
  parameters: { layout: 'padded' },
  decorators: [
    (Story) => (
      <div className="w-[680px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof SeasonPanel>;

export const AiringSeason: Story = {
  args: { season: FIXTURE_SEASON_AIRING },
};

export const Specials: Story = {
  args: { season: FIXTURE_SEASON_SPECIALS },
};

export const SpecialsForceOpen: Story = {
  args: { season: FIXTURE_SEASON_SPECIALS, defaultOpen: true },
};
