import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import type { Status } from '@/lib/status';

import { StatusFilterDropdown } from './StatusFilterDropdown';

const meta: Meta<typeof StatusFilterDropdown> = {
  title: 'Library/StatusFilterDropdown',
  component: StatusFilterDropdown,
  parameters: { layout: 'centered' },
};

export default meta;

type Story = StoryObj<typeof StatusFilterDropdown>;

export const Empty: Story = {
  args: {
    active: new Set(),
    onToggle: () => {},
  },
};

export const SingleActive: Story = {
  args: {
    active: new Set<Status>(['airing']),
    onToggle: () => {},
  },
};

export const SeveralActive: Story = {
  args: {
    active: new Set<Status>(['airing', 'incomplete']),
    counts: { airing: 4, incomplete: 12, complete: 86, untracked: 3, error: 1 },
    onToggle: () => {},
  },
};

export const Interactive: Story = {
  render: () => {
    function Inner() {
      const [active, setActive] = useState<Set<Status>>(new Set());
      return (
        <StatusFilterDropdown
          active={active}
          counts={{ airing: 4, incomplete: 12, complete: 86, untracked: 3, error: 1 }}
          onToggle={(s) => {
            setActive((prev) => {
              const next = new Set(prev);
              if (next.has(s)) {
                next.delete(s);
              } else {
                next.add(s);
              }
              return next;
            });
          }}
        />
      );
    }
    return <Inner />;
  },
};
