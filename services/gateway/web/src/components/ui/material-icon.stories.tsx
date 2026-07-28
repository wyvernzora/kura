import type { Meta, StoryObj } from '@storybook/react';

import { MaterialIcon } from './material-icon';

const meta: Meta<typeof MaterialIcon> = {
  title: 'Primitives/MaterialIcon',
  component: MaterialIcon,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof MaterialIcon>;

const SAMPLE = [
  'flag',
  'filter_alt',
  'filter_alt_off',
  'sort',
  'tune',
  'play_arrow',
  'pause',
  'check',
  'close',
  'error',
  'warning',
  'info',
];

export const Default: Story = {
  args: { name: 'flag' },
};

export const Sizes: Story = {
  render: () => (
    <div className="flex items-end gap-4 text-ink">
      {[14, 16, 18, 20, 24, 32].map((size) => (
        <div key={size} className="flex flex-col items-center gap-1">
          <MaterialIcon name="flag" size={size} />
          <span className="font-mono text-[10px] text-muted">{size}px</span>
        </div>
      ))}
    </div>
  ),
};

/**
 * Subset gallery — useful catalog when you need to pick a glyph for
 * a new affordance. Material Symbols ships thousands; this is the
 * shortlist of common kura UI verbs.
 */
export const Gallery: Story = {
  render: () => (
    <div className="grid grid-cols-6 gap-3 text-ink">
      {SAMPLE.map((name) => (
        <div
          key={name}
          className="flex flex-col items-center gap-1 rounded-md bg-surface p-3 shadow-card"
        >
          <MaterialIcon name={name} size={20} />
          <span className="font-mono text-[10px] text-muted">{name}</span>
        </div>
      ))}
    </div>
  ),
};
