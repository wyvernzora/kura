import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import type { TrashEmpty, TrashList } from '@/api/types.gen';
import { withStoryProviders } from '@/components/_storyProviders';
import { StoryRouter } from '@/components/_storyRouter';
import { FIXTURE_TRASH_GROUPS, trashListFixture } from '@/components/_trashFixtures';
import { TrashPage } from '@/components/TrashPage';
import { useTrashView } from '@/state/trashView';

/**
 * The page reads its listing through `useTrashList`, so every story
 * seeds the Storybook fetch mock (`.storybook/fetchMock.ts`) rather
 * than passing props: that exercises the real query → render path.
 *
 * `emptyResult` seeds the response the DELETE routes answer with, which
 * is how the three result-banner outcomes are demoed.
 */
function Harness({
  list,
  emptyResult,
  maxAgeHours = 0,
}: {
  /** `'never'` hangs the listing request — pins the loading skeleton. */
  list?: TrashList | 'never';
  /** `'never'` hangs the DELETE — pins the emptying-in-flight banner. */
  emptyResult?: TrashEmpty | { status: number; body: unknown } | 'never';
  maxAgeHours?: number;
}) {
  // Lazy init runs once, before the query hook's first fetch.
  useState(() => {
    // biome-ignore lint/suspicious/noExplicitAny: storybook-only mock seam
    const w = window as any;
    w.__kuraTrashList__ = list ?? trashListFixture();
    w.__kuraTrashEmpty__ = emptyResult;
    useTrashView.setState({ maxAgeHours });
    return null;
  });
  return (
    <StoryRouter initialPath="/trash">
      <div className="min-h-[760px] bg-paper">
        <TrashPage />
      </div>
    </StoryRouter>
  );
}

const meta: Meta<typeof TrashPage> = {
  title: 'Pages/Trash',
  component: TrashPage,
  parameters: { layout: 'fullscreen' },
  decorators: [withStoryProviders],
};

/**
 * Plays run at mount — before the listing query resolves (the header
 * button is disabled until then) and before radix portals mount — so
 * fixed timeouts race. Poll for the target until it is clickable.
 */
async function clickWhenReady(find: () => HTMLButtonElement | null | undefined): Promise<void> {
  for (let attempt = 0; attempt < 60; attempt++) {
    const el = find();
    if (el && !el.disabled) {
      el.click();
      return;
    }
    await new Promise((r) => setTimeout(r, 50));
  }
  throw new Error('play: target button never became clickable');
}

const buttonByText = (root: ParentNode, text: string) =>
  Array.from(root.querySelectorAll('button')).find((b) => b.textContent?.includes(text));

export default meta;
type Story = StoryObj<typeof TrashPage>;

/** Default view — full listing, no age filter, size-descending. */
export const FullListing: Story = {
  render: () => <Harness />,
};

/**
 * Listing still loading — shimmer stubs for the stats and rows.
 * Terabyte-scale trash takes the server a few seconds to walk, and a
 * blank page would read as "no trash".
 */
export const Loading: Story = {
  render: () => <Harness list="never" />,
};

/**
 * Empty mutation in flight — the spinner banner holds the gap between
 * the confirm closing and the result landing; every Empty affordance
 * is disabled meanwhile.
 */
export const Emptying: Story = {
  render: () => <Harness emptyResult="never" />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => buttonByText(canvasElement, 'Empty trash'));
    // The confirm dialog portals outside the canvas.
    await clickWhenReady(() => buttonByText(document, 'Empty permanently'));
  },
};

/** Nothing held at all. Stats render em dashes; pills are absent. */
export const TrashEmptyState: Story = {
  render: () => <Harness list={trashListFixture([])} />,
};

/**
 * Trash exists but the age filter excludes all of it — the second,
 * distinct empty state, which offers a way back out of the filter.
 */
export const NoEntriesMatch: Story = {
  render: () => (
    <Harness
      list={trashListFixture(FIXTURE_TRASH_GROUPS.slice(0, 1))}
      // Frieren's oldest entry is 41 days old, so a 90-day floor
      // empties the listing without emptying the trash.
      maxAgeHours={24 * 90}
    />
  ),
};

/**
 * Detail panel open. Click any series row; ONE PIECE is the deep case
 * with 24 entries, mixed quality chips and companion disclosures.
 */
export const DetailPanel: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() =>
      canvasElement.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]'),
    );
  },
};

/** Confirm gate for the page-level action, showing the scope block. */
export const ConfirmEmptyAll: Story = {
  render: () => <Harness maxAgeHours={24 * 7} />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => buttonByText(canvasElement, 'Empty trash'));
  },
};

/** Success banner — self-expires after 6 s with the drain bar. */
export const ResultSuccess: Story = {
  render: () => (
    <Harness
      emptyResult={{
        series: [],
        totalEntries: 34,
        attempts: 34,
        reclaimedBytes: 31_800_000_000,
      }}
    />
  ),
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => buttonByText(canvasElement, 'Empty trash'));
    // The confirm dialog portals outside the canvas.
    await clickWhenReady(() => buttonByText(document, 'Empty permanently'));
  },
};

/** Partial banner — never auto-dismisses; one mono line per failure. */
export const ResultPartial: Story = {
  render: () => (
    <Harness
      emptyResult={{
        series: [],
        totalEntries: 21,
        attempts: 24,
        reclaimedBytes: 19_100_000_000,
        failures: [
          {
            directory: 'Cowboy Bebop',
            error: 'workflow: trash empty Cowboy Bebop/01JZ8M…: permission denied',
          },
          {
            directory: 'Monster',
            error: 'workflow: trash empty Monster/01JT4K…: device or resource busy',
          },
        ],
      }}
    />
  ),
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => buttonByText(canvasElement, 'Empty trash'));
    // The confirm dialog portals outside the canvas.
    await clickWhenReady(() => buttonByText(document, 'Empty permanently'));
  },
};

/** Nothing-matched banner — the sweep ran but attempted no deletions. */
export const ResultNothingMatched: Story = {
  render: () => (
    <Harness
      maxAgeHours={24 * 90}
      emptyResult={{ series: [], totalEntries: 0, attempts: 0, reclaimedBytes: 0 }}
    />
  ),
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => buttonByText(canvasElement, 'Empty trash'));
    // The confirm dialog portals outside the canvas.
    await clickWhenReady(() => buttonByText(document, 'Empty permanently'));
  },
};
