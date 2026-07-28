import type { Meta, StoryObj } from '@storybook/react';

import { PosterArt } from './PosterArt';

const meta: Meta<typeof PosterArt> = {
  title: 'Primitives/PosterArt',
  component: PosterArt,
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div className="aspect-[0.7] w-[180px] overflow-hidden rounded-[12px] shadow-poster">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof PosterArt>;

/**
 * The fallback art is deterministic — same title hashes to the same
 * palette + composition + glyphs every render. The gallery below
 * exercises a spread of titles so the hash distribution shows.
 */

export const ShortLatin: Story = {
  args: { title: 'Frieren' },
};

export const LongLatin: Story = {
  args: { title: 'Re:Zero — Starting Life in Another World' },
};

export const Japanese: Story = {
  args: { title: '葬送のフリーレン' },
};

export const TraditionalChinese: Story = {
  args: { title: '葬送的芙莉蓮' },
};

export const Numeric: Story = {
  args: { title: '86 — Eighty Six' },
};

/** Small grid showing spread across hash buckets. */
export const Gallery: Story = {
  decorators: [],
  render: () => (
    <div className="grid grid-cols-4 gap-3">
      {[
        'Frieren',
        'Cowboy Bebop',
        'Re:Zero',
        'Mushishi',
        'Houseki no Kuni',
        'Vinland Saga',
        'Berserk',
        '86',
      ].map((title) => (
        <div key={title} className="flex flex-col gap-1.5">
          <div className="aspect-[0.7] w-[140px] overflow-hidden rounded-[12px] shadow-poster">
            <PosterArt title={title} />
          </div>
          <span className="font-mono text-[10px] text-muted">{title}</span>
        </div>
      ))}
    </div>
  ),
};
