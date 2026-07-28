import type { Meta, StoryObj } from '@storybook/react';

import type { StatusValue } from '@/lib/status';

import { Poster } from './poster';

const meta: Meta<typeof Poster> = {
  title: 'Primitives/Poster',
  component: Poster,
  parameters: { layout: 'centered' },
  args: {
    title: '葬送のフリーレン',
    status: 'airing',
    available: 4,
    total: 28,
    onClick: () => {},
  },
  decorators: [
    (Story) => (
      <div className="w-[160px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof Poster>;

export const Airing: Story = {};

export const Complete: Story = {
  args: {
    title: 'PLUTO',
    status: 'complete',
    available: 8,
    total: 8,
  },
};

export const Incomplete: Story = {
  args: {
    title: 'SPY×FAMILY Season 2',
    status: 'incomplete',
    available: 22,
    total: 25,
  },
};

export const HighPriority: Story = {
  args: {
    tags: ['priority:high'],
  },
};

export const LowPriority: Story = {
  args: {
    tags: ['priority:low'],
  },
};

export const DisabledPriority: Story = {
  args: {
    tags: ['maintenance:disabled'],
  },
};

export const AiringIncomplete: Story = {
  args: {
    title: '怪獣8号',
    status: ['airing', 'incomplete'] satisfies StatusValue,
    available: 6,
    total: 12,
  },
};

export const Untracked: Story = {
  args: {
    title: 'Mystery folder',
    status: 'untracked',
    available: undefined,
    total: undefined,
  },
};

export const ErrorState: Story = {
  args: {
    title: 'ゆびさきと恋々',
    status: 'error',
    available: 0,
    total: 12,
  },
};

/**
 * Forced :hover via the pseudo-states addon — should lift -3 px and
 * deepen the shadow from poster to poster-hover. Pair with the theme
 * dropdown to verify both palettes.
 *
 * Note: the cursor-tracked tilt only fires on real mouse-move; the
 * pseudo-states addon can't simulate cursor position. Use the `Tilt`
 * story (or the live app) to exercise that branch.
 */
export const Hovered: Story = {
  parameters: { pseudo: { hover: true } },
};

/**
 * Hover and move the cursor across the card to see the AppleTV-style
 * tilt — max ±5° on each axis, snaps back on mouseleave.
 */
export const Tilt: Story = {
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div className="grid w-[260px] place-items-center">
        <Story />
        <p className="mt-4 max-w-[220px] text-center text-xs text-muted">
          Hover and drift the cursor — card rotates toward it. Snaps back on leave.
        </p>
      </div>
    ),
  ],
  args: { className: 'w-[200px]' },
};

export const Dense: Story = {
  args: { dense: true },
};

export const HideTitle: Story = {
  args: { hideTitle: true, noHover: true, onClick: undefined },
};

const SAMPLES: {
  title: string;
  status: StatusValue;
  a: number;
  t: number;
}[] = [
  { title: '葬送のフリーレン', status: 'airing', a: 4, t: 28 },
  { title: 'PLUTO', status: 'complete', a: 8, t: 8 },
  { title: 'チェンソーマン', status: 'complete', a: 12, t: 12 },
  { title: '怪獣8号', status: ['airing', 'incomplete'], a: 6, t: 12 },
  { title: 'SPY×FAMILY S2', status: 'incomplete', a: 22, t: 25 },
  { title: 'Mystery', status: 'untracked', a: 0, t: 0 },
];

export const Grid: Story = {
  parameters: { layout: 'fullscreen' },
  decorators: [],
  render: () => (
    <div className="bg-paper p-6">
      <div className="mx-auto grid w-[920px] grid-cols-6 gap-4">
        {SAMPLES.map((s) => (
          <Poster
            key={s.title}
            title={s.title}
            status={s.status}
            available={s.a}
            total={s.t}
            onClick={() => {}}
          />
        ))}
      </div>
    </div>
  ),
};
