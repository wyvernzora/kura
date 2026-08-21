import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import type { ReleaseItem } from '@/api/releaseTypes';
import type { ListRow } from '@/api/types';
import { FIXTURE_RELEASE_SERIES, FIXTURE_RELEASES } from '@/components/_releaseFixtures';
import { withStoryProviders } from '@/components/_storyProviders';
import { StoryRouter } from '@/components/_storyRouter';
import { ReleasesPage } from '@/components/ReleasesPage';
import type { ReleaseFilterKey } from '@/state/releaseStreams';
import { useReleasesView } from '@/state/releasesView';

/**
 * The page reads everything through its query hooks, so every story
 * seeds the Storybook fetch mock (`.storybook/fetchMock.ts`) rather
 * than passing props: that exercises the real two-stream fetch →
 * merge → render path, including "Load more".
 */
function Harness({
  releases,
  series,
  setStatus,
  filter,
}: {
  releases?: ReleaseItem[];
  series?: ListRow[];
  setStatus?: { status: number; body: unknown };
  filter?: ReleaseFilterKey[];
}) {
  // Lazy init runs once, before the query hooks' first fetch.
  useState(() => {
    // biome-ignore lint/suspicious/noExplicitAny: storybook-only mock seam
    const w = window as any;
    w.__kuraReleases__ = releases;
    w.__kuraSeriesList__ = series ?? FIXTURE_RELEASE_SERIES;
    w.__kuraSetStatus__ = setStatus;
    useReleasesView.setState({ active: new Set(filter ?? ['exhausted', 'lowconf']) });
    return null;
  });
  return (
    <StoryRouter initialPath="/releases">
      <div className="min-h-[760px] bg-paper">
        <ReleasesPage />
      </div>
    </StoryRouter>
  );
}

const meta: Meta<typeof ReleasesPage> = {
  title: 'Pages/Releases',
  component: ReleasesPage,
  parameters: { layout: 'fullscreen' },
  decorators: [withStoryProviders],
};

export default meta;
type Story = StoryObj<typeof ReleasesPage>;

/**
 * Plays run at mount — before the list queries resolve and before
 * radix portals mount — so fixed timeouts race. Poll for the target
 * until it is clickable.
 */
async function clickWhenReady(find: () => HTMLElement | null | undefined): Promise<void> {
  for (let attempt = 0; attempt < 60; attempt++) {
    const el = find();
    if (el && !(el as HTMLButtonElement).disabled) {
      el.click();
      return;
    }
    await new Promise((r) => setTimeout(r, 50));
  }
  throw new Error('play: target never became clickable');
}

const byText = (root: ParentNode, text: string) =>
  Array.from(root.querySelectorAll('button')).find((b) => b.textContent?.includes(text));

/**
 * The confirm dialog portals outside the canvas AND repeats the label
 * of the button that opened it, so a document-wide search finds the
 * page's button again — scope to the dialog.
 */
const inDialog = (text: string) => {
  const dialog = document.querySelector('[role="dialog"]');
  return dialog ? byText(dialog, text) : undefined;
};

/** Opens the exhausted release the action stories act on. */
const openExhausted = (canvas: ParentNode) => byText(canvas, 'Attack on Titan S04E29');

/** Default view: the attention set — exhausted plus low-confidence matches. */
export const AttentionQueue: Story = {
  render: () => <Harness />,
};

/** Every status, the widest the filter menu goes. */
export const AllStatuses: Story = {
  render: () => <Harness filter={[]} />,
};

/** Suppressed on its own — inspection material, hand match the only way out. */
export const SuppressedOnly: Story = {
  render: () => <Harness filter={['suppressed']} />,
};

/** Nothing needs attention — the empty state and its way out of the filter. */
export const NothingToDo: Story = {
  render: () => (
    <Harness
      releases={FIXTURE_RELEASES.filter(
        (r) => r.matchStatus !== 'exhausted' && (r.confidence ?? 1) >= 0.75,
      )}
    />
  ),
};

/** Detail view: field grid, both evidence lists, and the per-state actions. */
export const ExhaustedDetail: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => openExhausted(canvasElement));
  },
};

/** Hand match: library search, poster candidates, then the ref confirm step. */
export const HandMatch: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => openExhausted(canvasElement));
    await clickWhenReady(() => byText(canvasElement, 'Hand match'));
  },
};

/** Requeue confirm — the exhaustion accounting it clears is named. */
export const RequeueConfirm: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => openExhausted(canvasElement));
    await clickWhenReady(() => byText(canvasElement, 'Requeue'));
  },
};

/** Success toast — self-expires after 6 s with the drain bar. */
export const ActionSucceeded: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => openExhausted(canvasElement));
    await clickWhenReady(() => byText(canvasElement, 'Requeue'));
    await clickWhenReady(() => inDialog('Requeue'));
  },
};

/**
 * 409 invalid transition — the API's message is surfaced verbatim, and
 * the error toast stays until dismissed.
 */
export const TransitionRejected: Story = {
  render: () => (
    <Harness
      setStatus={{
        status: 409,
        body: {
          kind: 'conflict',
          message:
            'invalid transition: exhausted → unmatched (release b1d0c2e3…); status changed since this page was loaded',
        },
      }}
    />
  ),
  play: async ({ canvasElement }) => {
    await clickWhenReady(() => openExhausted(canvasElement));
    await clickWhenReady(() => byText(canvasElement, 'Requeue'));
    await clickWhenReady(() => inDialog('Requeue'));
  },
};
