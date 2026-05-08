import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { SearchField } from './SearchField';

const meta: Meta<typeof SearchField> = {
  title: 'Chrome/SearchField',
  component: SearchField,
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div className="w-[480px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof SearchField>;

export const Empty: Story = {
  args: {
    placeholder: 'Search library — title, source, year…',
  },
};

export const WithValue: Story = {
  render: () => {
    function Inner() {
      const [value, setValue] = useState('frieren');
      return (
        <SearchField
          value={value}
          placeholder="Search library — title, source, year…"
          onChange={(e) => setValue(e.target.value)}
          onClear={() => setValue('')}
        />
      );
    }
    return <Inner />;
  },
};

/**
 * Forced focus-within — exercises the kura-focusable focus glow
 * (blue ring + halo) without needing the user to click into the
 * input. Compare against Empty in the same theme to see the change.
 */
export const Focused: Story = {
  parameters: { pseudo: { focusWithin: true } },
  args: {
    placeholder: 'Search library — title, source, year…',
  },
};

export const Disabled: Story = {
  args: {
    disabled: true,
    placeholder: 'Search library — title, source, year…',
  },
};
