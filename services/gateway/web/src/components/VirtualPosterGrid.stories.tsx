import type { Meta, StoryObj } from '@storybook/react';

import { pickDensity } from '@/lib/useAutoDensity';
import { FIXTURE_LIST_ROWS } from './_listFixtures';
import { StoryRouter } from './_storyRouter';
import { VirtualPosterGrid } from './VirtualPosterGrid';

const meta: Meta<typeof VirtualPosterGrid> = {
  title: 'Library/VirtualPosterGrid',
  component: VirtualPosterGrid,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof VirtualPosterGrid>;

const DENSITY_LG = pickDensity(1400);
const DENSITY_MD = pickDensity(900);
const DENSITY_SM = pickDensity(600);

/**
 * VirtualPosterGrid uses the *window* scroller — it expects to live
 * inside a scrollable page chrome, not a story-frame iframe overflow.
 * Each story wraps in a StoryRouter (Poster click handler navigates)
 * and a narrow container that simulates the live page width.
 */

export const LargeViewport: Story = {
  render: () => (
    <StoryRouter>
      <div className="min-h-dvh bg-paper px-6 py-6">
        <VirtualPosterGrid rows={FIXTURE_LIST_ROWS} density={DENSITY_LG} />
      </div>
    </StoryRouter>
  ),
};

export const MediumViewport: Story = {
  render: () => (
    <StoryRouter>
      <div className="mx-auto min-h-dvh max-w-[900px] bg-paper px-6 py-6">
        <VirtualPosterGrid rows={FIXTURE_LIST_ROWS} density={DENSITY_MD} />
      </div>
    </StoryRouter>
  ),
};

export const SmallViewport: Story = {
  render: () => (
    <StoryRouter>
      <div className="mx-auto min-h-dvh max-w-[420px] bg-paper px-4 py-4">
        <VirtualPosterGrid rows={FIXTURE_LIST_ROWS} density={DENSITY_SM} />
      </div>
    </StoryRouter>
  ),
};

/** Empty state — no rows, no DOM cost, layout collapses to nothing. */
export const Empty: Story = {
  render: () => (
    <StoryRouter>
      <div className="min-h-dvh bg-paper px-6 py-6">
        <VirtualPosterGrid rows={[]} density={DENSITY_LG} />
        <p className="mt-4 text-sm text-muted">No rows. Grid renders nothing.</p>
      </div>
    </StoryRouter>
  ),
};
