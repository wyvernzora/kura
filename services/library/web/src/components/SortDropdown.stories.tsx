import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import type { SortSpec } from '@/lib/library';

import { SortDropdown } from './SortDropdown';

const meta: Meta<typeof SortDropdown> = {
  title: 'Library/SortDropdown',
  component: SortDropdown,
  parameters: { layout: 'centered' },
};

export default meta;

type Story = StoryObj<typeof SortDropdown>;

export const Default: Story = {
  render: () => {
    function Inner() {
      const [value, setValue] = useState<SortSpec>({ key: 'title', direction: 'asc' });
      return <SortDropdown value={value} onChange={setValue} />;
    }
    return <Inner />;
  },
};

export const StatusSort: Story = {
  render: () => {
    function Inner() {
      const [value, setValue] = useState<SortSpec>({ key: 'status', direction: 'asc' });
      return <SortDropdown value={value} onChange={setValue} />;
    }
    return <Inner />;
  },
};
