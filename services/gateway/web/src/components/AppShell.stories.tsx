import type { Meta, StoryObj } from '@storybook/react';
import { withStoryProviders } from './_storyProviders';
import { StoryRouter } from './_storyRouter';
import { AppShell } from './AppShell';

const meta: Meta<typeof AppShell> = {
  title: 'Compositions/AppShell',
  component: AppShell,
  parameters: { layout: 'fullscreen' },
  // TopBar prefetches the TVDB resolve through react-query, so the
  // shell needs a QueryClient in context to render at all.
  decorators: [withStoryProviders],
};

export default meta;
type Story = StoryObj<typeof AppShell>;

/** Library home — sidebar rail beside the top bar and stub content. */
export const LibraryHome: Story = {
  render: () => (
    <StoryRouter initialPath="/">
      <AppShell>
        <div className="mx-auto max-w-3xl px-6 py-12 text-muted text-sm">
          Stub library content. Real app renders the virtualized poster grid here.
        </div>
      </AppShell>
    </StoryRouter>
  ),
};

/** Detail route — the top bar's leading slot gains a back-to-library pill. */
export const DetailRoute: Story = {
  render: () => (
    <StoryRouter initialPath="/series/tvdb:424536">
      <AppShell>
        <div className="mx-auto max-w-3xl px-6 py-12 text-muted text-sm">
          Stub detail content. Real app renders the SeriesDetail composition here.
        </div>
      </AppShell>
    </StoryRouter>
  ),
};

/**
 * Trash route — like Settings, a searchless page: no top bar on
 * desktop, and the slim mobile bar titled "Trash" below `md`.
 */
export const TrashRoute: Story = {
  render: () => (
    <StoryRouter initialPath="/trash">
      <AppShell>
        <div className="mx-auto max-w-3xl px-6 py-12 text-muted text-sm">
          Stub trash content. Real app renders the Trash page here.
        </div>
      </AppShell>
    </StoryRouter>
  ),
};

/**
 * Settings route — no top bar at all on desktop (the slim mobile bar
 * that replaces it only renders below `md`).
 */
export const SettingsRoute: Story = {
  render: () => (
    <StoryRouter initialPath="/settings">
      <AppShell>
        <div className="mx-auto max-w-3xl px-6 py-12 text-muted text-sm">
          Stub settings content. Real app renders the Settings page here.
        </div>
      </AppShell>
    </StoryRouter>
  ),
};
