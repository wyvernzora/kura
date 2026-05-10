import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { withStoryProviders } from '@/components/_storyProviders';
import { ScanButton } from '@/components/series/ScanButton';
import { clearScanRecord, type ScanRecord, writeScanRecord } from '@/lib/scanState';

/**
 * The scan-button visual states are driven entirely by what's in
 * localStorage when `useScanJob` mounts. Each story seeds its own
 * unique metadataRef so the four states render simultaneously without
 * fighting over the same key.
 *
 * The hook polls `/api/v1/jobs/{id}` while running. The Storybook
 * preview installs a global fetch mock for `/api/v1/series/*\/scan`
 * + `/api/v1/jobs/*` so clicking the button in a story exercises the
 * full kickoff → poll → terminal cycle without a real backend.
 */

interface HarnessProps {
  storyKey: string;
  initial?: ScanRecord;
  lastScanned?: string;
}

function Harness({ storyKey, initial, lastScanned }: HarnessProps) {
  // Lazy init runs ONCE before any child useState (incl. the hook's
  // own `useState(() => readScanRecord(ref))`). The side-effect is
  // intentional: the record must be in localStorage before the hook
  // reads it, otherwise the story always renders idle on first paint.
  const [ref] = useState(() => {
    const r = `tvdb:storybook-${storyKey}`;
    if (initial) {
      writeScanRecord(r, initial);
    } else {
      clearScanRecord(r);
    }
    return r;
  });
  return <ScanButton metadataRef={ref} lastScanned={lastScanned} onShowDetails={() => {}} />;
}

const meta: Meta<typeof ScanButton> = {
  title: 'Series/ScanButton',
  component: ScanButton,
  parameters: { layout: 'centered' },
  decorators: [
    withStoryProviders,
    (Story) => (
      <div className="w-[300px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ScanButton>;

export const Idle: Story = {
  render: () => (
    <Harness
      storyKey="idle"
      lastScanned={new Date(Date.now() - 4 * 60 * 60 * 1000).toISOString()}
    />
  ),
};

export const NeverScanned: Story = {
  render: () => <Harness storyKey="never" />,
};

export const Running: Story = {
  render: () => (
    <Harness
      storyKey="running"
      initial={{
        state: 'running',
        jobId: 'sb-running-1',
        startedAt: new Date().toISOString(),
      }}
    />
  ),
};

export const Warning: Story = {
  render: () => (
    <Harness
      storyKey="warning"
      initial={{
        state: 'warning',
        jobId: 'sb-warning-1',
        finishedAt: new Date().toISOString(),
        skipped: [
          {
            path: 'Season 01/extra-bonus.mkv',
            code: 'episode_number_not_inferred',
            reason: 'no SXEYY pattern in filename',
          },
          {
            path: 'Season 02/clip-show.mkv',
            code: 'episode_number_not_inferred',
            reason: 'no SXEYY pattern in filename',
          },
          {
            path: 'Season 02/S03E01.mkv',
            code: 'season_mismatch',
            reason: 'filename season 03 does not match dir Season 02',
          },
        ],
      }}
    />
  ),
};

export const Errored: Story = {
  render: () => (
    <Harness
      storyKey="error"
      initial={{
        state: 'error',
        jobId: 'sb-error-1',
        finishedAt: new Date().toISOString(),
        progressFrozen: 0.42,
        error: {
          kind: 'internal',
          message: 'mediainfo: exec failed: signal killed',
        },
      }}
    />
  ),
};

/**
 * All four states stacked so the visual hierarchy is reviewable at a
 * glance — the hairline color jumps from ink (running) to amber
 * (warning) to red (error), and the caption row swaps between
 * counter / message / link.
 */
export const Stack: Story = {
  render: () => (
    <div className="flex flex-col gap-6">
      <Harness
        storyKey="stack-idle"
        lastScanned={new Date(Date.now() - 4 * 60 * 60 * 1000).toISOString()}
      />
      <Harness
        storyKey="stack-running"
        initial={{
          state: 'running',
          jobId: 'sb-stack-running',
          startedAt: new Date().toISOString(),
        }}
      />
      <Harness
        storyKey="stack-warning"
        initial={{
          state: 'warning',
          jobId: 'sb-stack-warning',
          finishedAt: new Date().toISOString(),
          skipped: [
            {
              path: 'a.mkv',
              code: 'episode_number_not_inferred',
              reason: 'no pattern',
            },
          ],
        }}
      />
      <Harness
        storyKey="stack-error"
        initial={{
          state: 'error',
          jobId: 'sb-stack-error',
          finishedAt: new Date().toISOString(),
          progressFrozen: 0.6,
          error: { kind: 'internal', message: 'mount unreachable' },
        }}
      />
    </div>
  ),
};
