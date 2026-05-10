import type { Meta, StoryObj } from '@storybook/react';
import { StoryRouter } from './_storyRouter';
import { AppShell } from './AppShell';

const meta: Meta<typeof AppShell> = {
  title: 'Compositions/AppShell',
  component: AppShell,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof AppShell>;

/** Library home — TopBar at-rest sits above stub content. */
export const LibraryHome: Story = {
  render: () => (
    <StoryRouter initialPath="/">
      <AppShell>
        <div className="mx-auto max-w-3xl px-6 py-12 text-sm text-muted">
          Stub library content. Real app renders the virtualized poster grid here.
        </div>
      </AppShell>
    </StoryRouter>
  ),
};

/** Detail route — TopBar swaps the logo for a back-to-library pill. */
export const DetailRoute: Story = {
  render: () => (
    <StoryRouter initialPath="/series/tvdb:424536">
      <AppShell>
        <div className="mx-auto max-w-3xl px-6 py-12 text-sm text-muted">
          Stub detail content. Real app renders the SeriesDetail composition here.
        </div>
      </AppShell>
    </StoryRouter>
  ),
};
