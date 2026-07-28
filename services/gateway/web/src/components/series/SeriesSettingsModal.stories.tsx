import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { withStoryProviders } from '@/components/_storyProviders';
import { SeriesSettingsModal } from '@/components/series/SeriesSettingsModal';

function ModalHarness({ tags }: { tags: string[] }) {
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
      <SeriesSettingsModal
        metadataRef="tvdb:370070"
        tags={tags}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
}

const meta: Meta<typeof SeriesSettingsModal> = {
  title: 'Series/SeriesSettingsModal',
  component: SeriesSettingsModal,
  parameters: { layout: 'fullscreen' },
  decorators: [withStoryProviders],
};

export default meta;
type Story = StoryObj<typeof SeriesSettingsModal>;

export const NoTags: Story = {
  render: () => <ModalHarness tags={[]} />,
};

export const LowPriorityMaintenanceRequested: Story = {
  render: () => (
    <ModalHarness tags={['fansub:goodgroup', 'priority:low', 'maintenance:requested']} />
  ),
};

export const HighPriority: Story = {
  render: () => <ModalHarness tags={['priority:high']} />,
};

export const Disabled: Story = {
  render: () => <ModalHarness tags={['maintenance:disabled']} />,
};
