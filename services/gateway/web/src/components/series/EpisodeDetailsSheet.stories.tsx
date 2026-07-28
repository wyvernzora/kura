import type { Meta, StoryObj } from '@storybook/react';
import { useRef, useState } from 'react';

import type { EpisodeShow } from '@/api/types';
import {
  FIXTURE_EPISODE_IN_PLACE,
  FIXTURE_EPISODE_LONG_PATHS,
  FIXTURE_EPISODE_MISSING,
  FIXTURE_EPISODE_PENDING,
  FIXTURE_EPISODE_PRESENT_RICH,
  FIXTURE_EPISODE_STAGED_ONLY,
  FIXTURE_EPISODE_STAGED_REPLACEMENT,
} from '@/components/series/_fixtures';
import { EpisodeDetailsSheet } from '@/components/series/EpisodeDetailsSheet';

const LAST_SCANNED = new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString();

function SheetHarness({ episode }: { episode: EpisodeShow }) {
  const [open, setOpen] = useState(true);
  const triggerRef = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen(true)}
        className="m-4 rounded-md bg-surface px-3 py-2 text-sm text-ink shadow-card"
      >
        Reopen episode details
      </button>
      <EpisodeDetailsSheet
        episode={episode}
        seriesDir="Frieren - Beyond Journeys End"
        lastScanned={LAST_SCANNED}
        open={open}
        onOpenChange={setOpen}
        restoreFocusRef={triggerRef}
      />
    </>
  );
}

const meta: Meta<typeof EpisodeDetailsSheet> = {
  title: 'Series/EpisodeDetailsSheet',
  component: EpisodeDetailsSheet,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof EpisodeDetailsSheet>;

export const Present: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_PRESENT_RICH} />,
};

export const StagedReplacement: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_STAGED_REPLACEMENT} />,
  play: () => {
    const stagedTab = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="tab"]')).find(
      (element) => element.textContent?.includes('Staged file'),
    );
    stagedTab?.click();
  },
};

export const StagedOnly: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_STAGED_ONLY} />,
};

export const InPlaceUpdate: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_IN_PLACE} />,
  play: () => {
    const stagedTab = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="tab"]')).find(
      (element) => element.textContent?.includes('Staged file'),
    );
    stagedTab?.click();
  },
};

export const Missing: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_MISSING} />,
};

export const Pending: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_PENDING} />,
};

export const LongPaths: Story = {
  render: () => <SheetHarness episode={FIXTURE_EPISODE_LONG_PATHS} />,
  parameters: { viewport: { defaultViewport: 'mobile1' } },
  play: () => {
    const stagedTab = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="tab"]')).find(
      (element) => element.textContent?.includes('Staged file'),
    );
    stagedTab?.click();
  },
};
