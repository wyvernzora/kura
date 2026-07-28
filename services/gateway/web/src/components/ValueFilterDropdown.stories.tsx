import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { ValueFilterDropdown } from './ValueFilterDropdown';

const meta: Meta<typeof ValueFilterDropdown> = {
  title: 'Library/ValueFilterDropdown',
  component: ValueFilterDropdown,
  parameters: { layout: 'centered' },
};

export default meta;

type Story = StoryObj<typeof ValueFilterDropdown>;

export const Source: Story = {
  args: {
    label: 'Source',
    icon: 'movie',
    values: ['BluRay', 'Web-DL', 'WebRip', 'HDTV'],
    counts: { BluRay: 42, 'Web-DL': 30, WebRip: 9, HDTV: 2 },
    active: new Set(),
    onToggle: () => {},
  },
};

export const Resolution: Story = {
  args: {
    label: 'Resolution',
    icon: 'aspect_ratio',
    values: ['2160p', '1080p', '720p'],
    counts: { '2160p': 4, '1080p': 70, '720p': 6 },
    active: new Set(['1080p']),
    onToggle: () => {},
  },
};

export const HidesZeroCounts: Story = {
  args: {
    label: 'Source',
    icon: 'movie',
    values: ['BluRay', 'Web-DL', 'WebRip', 'HDTV', 'DVDRip'],
    counts: { BluRay: 12, 'Web-DL': 5, WebRip: 0, HDTV: 0, DVDRip: 0 },
    active: new Set(),
    onToggle: () => {},
  },
};

export const DisabledWhenAllZero: Story = {
  args: {
    label: 'Source',
    icon: 'movie',
    values: ['BluRay', 'Web-DL', 'WebRip'],
    counts: {},
    active: new Set(),
    onToggle: () => {},
  },
};

export const Interactive: Story = {
  render: () => {
    function Inner() {
      const [active, setActive] = useState<Set<string>>(new Set());
      return (
        <ValueFilterDropdown
          label="Source"
          icon="movie"
          values={['BluRay', 'Web-DL', 'WebRip', 'HDTV']}
          counts={{ BluRay: 42, 'Web-DL': 30, WebRip: 9, HDTV: 2 }}
          active={active}
          onToggle={(v) => {
            setActive((prev) => {
              const next = new Set(prev);
              if (next.has(v)) {
                next.delete(v);
              } else {
                next.add(v);
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
