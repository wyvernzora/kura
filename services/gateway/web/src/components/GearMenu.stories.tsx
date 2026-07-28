import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { withStoryProviders } from '@/components/_storyProviders';
import { GearMenu } from '@/components/GearMenu';
import {
  clearLibraryJobRecord,
  type LibraryJobRecord,
  writeLibraryJobRecord,
} from '@/lib/libraryJobState';

/**
 * Gear menu states are driven by `kura.libraryJob` localStorage and
 * the `/api/v1/jobs/{id}` mock in `.storybook/fetchMock.ts`. The mock
 * peeks at localStorage on each poll: if a story seeded a record, it
 * returns mid-progress running state (current=312/total=742) so the
 * running view stays visible. Otherwise it returns terminal succeeded.
 *
 * The popout is opened by default in every story so reviewers see the
 * menu body without having to click the trigger. The trigger itself
 * still floats in the top-right of the harness so the gear glyph /
 * ring spinner is reviewable in the same frame.
 */

interface HarnessProps {
  initial?: LibraryJobRecord;
}

function Harness({ initial }: HarnessProps) {
  // Lazy init runs ONCE before any child useState (incl. the hook's
  // own `useState(() => readLibraryJobRecord())`). The side-effect is
  // intentional: the record must be in localStorage before the hook
  // reads it, otherwise the story always renders idle on first paint.
  useState(() => {
    if (initial) {
      writeLibraryJobRecord(initial);
    } else {
      clearLibraryJobRecord();
    }
    return null;
  });
  // Reserve enough vertical space for the popout (~340 px) plus the
  // 72 px top-bar slot the trigger normally lives in. Without it the
  // portaled menu lands below the storybook frame's visible area on
  // a tightly-cropped layout.
  return (
    <div className="relative h-[420px] w-full bg-paper">
      <div className="absolute top-4 right-6">
        <GearMenu defaultOpen />
      </div>
    </div>
  );
}

const meta: Meta<typeof GearMenu> = {
  title: 'Chrome/GearMenu',
  component: GearMenu,
  parameters: { layout: 'fullscreen' },
  decorators: [withStoryProviders],
};

export default meta;
type Story = StoryObj<typeof GearMenu>;

export const Idle: Story = {
  render: () => <Harness />,
};

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
