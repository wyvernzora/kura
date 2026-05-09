import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { ScanDetailsModal, type ScanDetailsView } from '@/components/series/ScanDetailsModal';

const SKIPPED_FEW = [
  {
    path: 'Season 01/extras-credits.mkv',
    code: 'episode_number_not_inferred',
    reason: 'no SXEYY pattern in filename',
  },
  {
    path: 'Season 02/S03E04.mkv',
    code: 'season_mismatch',
    reason: 'filename season 03 does not match dir Season 02',
  },
];

const SKIPPED_MANY = [
  ...SKIPPED_FEW,
  {
    path: 'Specials/clip-show.mkv',
    code: 'special_number_not_inferred',
    reason: 'specials dir lacks S00E# pattern',
  },
  {
    path: 'Season 01/S01E12 [v1].mkv',
    code: 'duplicate_slot',
    reason: 'shares slot with Season 01/S01E12 [v2].mkv',
  },
  {
    path: 'Season 01/S01E12 [v2].mkv',
    code: 'duplicate_slot',
    reason: 'shares slot with Season 01/S01E12 [v1].mkv',
  },
  {
    path: 'Season 02/extras',
    code: 'ignored_directory',
    reason: 'directory is not a season directory',
  },
  {
    path: 'Season 03/S03E99.mkv',
    code: 'metadata_slot_missing',
    reason: 'metadata has no S03E0099',
  },
];

function ModalHarness({ initialView }: { initialView: ScanDetailsView }) {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="rounded-md bg-surface px-3 py-1 text-sm shadow-card"
      >
        Reopen
      </button>
      <ScanDetailsModal open={open} onOpenChange={setOpen} view={initialView} />
    </>
  );
}

const meta: Meta<typeof ScanDetailsModal> = {
  title: 'Series/ScanDetailsModal',
  component: ScanDetailsModal,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof ScanDetailsModal>;

export const WarningFew: Story = {
  render: () => <ModalHarness initialView={{ kind: 'warning', skipped: SKIPPED_FEW }} />,
};

export const WarningManyGrouped: Story = {
  render: () => <ModalHarness initialView={{ kind: 'warning', skipped: SKIPPED_MANY }} />,
};

export const ErrorBasic: Story = {
  render: () => (
    <ModalHarness
      initialView={{
        kind: 'error',
        progressFrozen: 0.42,
        error: { kind: 'internal', message: 'mediainfo: exec failed: signal killed' },
      }}
    />
  ),
};

export const ErrorWithData: Story = {
  render: () => (
    <ModalHarness
      initialView={{
        kind: 'error',
        progressFrozen: 0.85,
        error: {
          kind: 'busy',
          message: 'series is busy: scan on host=container-1 pid=42 since 12s ago',
          data: {
            scope: 'series',
            holder: { host: 'container-1', pid: 42, op: 'scan' },
          },
        },
      }}
    />
  ),
};
