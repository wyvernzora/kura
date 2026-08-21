import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { withStoryProviders } from '@/components/_storyProviders';
import { StoryRouter } from '@/components/_storyRouter';
import { SettingsPage } from '@/components/SettingsPage';
import {
  clearLibraryJobRecord,
  type LibraryJobRecord,
  writeLibraryJobRecord,
} from '@/lib/libraryJobState';

/**
 * The maintenance card's running view is driven by the
 * `kura.libraryJob` localStorage record plus the
 * `/api/library/v1/jobs/{id}` mock in `.storybook/fetchMock.ts`: when a
 * story seeds a record, the mock keeps answering with mid-progress
 * running state (312/742) so the progress view stays on screen.
 */
function Harness({ initial }: { initial?: LibraryJobRecord }) {
  // Lazy init runs ONCE before the hook's own state initializer, so
  // the record is in localStorage by the time `useLibraryJob` reads it.
  useState(() => {
    if (initial) {
      writeLibraryJobRecord(initial);
    } else {
      clearLibraryJobRecord();
    }
    return null;
  });
  return (
    <StoryRouter initialPath="/settings">
      <div className="min-h-[620px] bg-paper">
        <SettingsPage />
      </div>
    </StoryRouter>
  );
}

const meta: Meta<typeof SettingsPage> = {
  title: 'Pages/Settings',
  component: SettingsPage,
  parameters: { layout: 'fullscreen' },
  decorators: [withStoryProviders],
};

export default meta;
type Story = StoryObj<typeof SettingsPage>;

/** Idle — both maintenance rows offer their run button. */
export const Idle: Story = {
  render: () => <Harness />,
};

/** Scan running — progress replaces the maintenance rows wholesale. */
export const RunningScan: Story = {
  render: () => (
    <Harness
      initial={{
        state: 'running',
        kind: 'scan',
        jobId: 'sb-libjob-scan',
        startedAt: new Date().toISOString(),
      }}
    />
  ),
};

/** Reindex running — same view, different job label. */
export const RunningReindex: Story = {
  render: () => (
    <Harness
      initial={{
        state: 'running',
        kind: 'reindex',
        jobId: 'sb-libjob-reindex',
        startedAt: new Date().toISOString(),
      }}
    />
  ),
};
