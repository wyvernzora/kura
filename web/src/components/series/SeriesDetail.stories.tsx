import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Show } from '@/api/types';
import { withStoryProviders } from '@/components/_storyProviders';
import { StoryRouter } from '@/components/_storyRouter';
import { FIXTURE_SHOW_AIRING, FIXTURE_SHOW_COMPLETE_SINGLE } from './_fixtures';
import { SeriesDetail } from './SeriesDetail';

/**
 * `SeriesDetail` calls `useShow(ref)` which goes through TanStack Query.
 * We seed the cache with a fixture so the success branch renders without
 * touching the network. The pending and error branches each get their
 * own client so the cache state matches what the component needs to see.
 */
function withSeededShow(ref: string, show: Show) {
  return function Decorator(Story: () => JSX.Element) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Number.POSITIVE_INFINITY } },
    });
    client.setQueryData(['series', 'show', ref], show);
    return (
      <StoryRouter initialPath={`/series/${ref}`}>
        <QueryClientProvider client={client}>
          <Story />
        </QueryClientProvider>
      </StoryRouter>
    );
  };
}

function withErrorShow(ref: string) {
  return function Decorator(Story: () => JSX.Element) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    // biome-ignore lint/suspicious/noExplicitAny: synthetic error injection
    (client as any).setQueryData(['series', 'show', ref], () => {
      throw new Error('series resolve failed: provider returned 502');
    });
    return (
      <StoryRouter initialPath={`/series/${ref}`}>
        <QueryClientProvider client={client}>
          <Story />
        </QueryClientProvider>
      </StoryRouter>
    );
  };
}

const meta: Meta<typeof SeriesDetail> = {
  title: 'Compositions/SeriesDetail',
  component: SeriesDetail,
  parameters: { layout: 'fullscreen' },
  decorators: [withStoryProviders],
};

export default meta;
type Story = StoryObj<typeof SeriesDetail>;

export const AiringMultiSeason: Story = {
  decorators: [withSeededShow(FIXTURE_SHOW_AIRING.metadataRef, FIXTURE_SHOW_AIRING)],
  render: () => (
    <div className="min-h-dvh bg-paper">
      <SeriesDetail seriesRef={FIXTURE_SHOW_AIRING.metadataRef} />
    </div>
  ),
};

export const CompleteSingleSeason: Story = {
  decorators: [
    withSeededShow(FIXTURE_SHOW_COMPLETE_SINGLE.metadataRef, FIXTURE_SHOW_COMPLETE_SINGLE),
  ],
  render: () => (
    <div className="min-h-dvh bg-paper">
      <SeriesDetail seriesRef={FIXTURE_SHOW_COMPLETE_SINGLE.metadataRef} />
    </div>
  ),
};

/** Pending branch — no fixture seeded, TanStack Query stays in
 *  isPending and the skeleton renders. */
export const Pending: Story = {
  decorators: [
    (Story) => (
      <StoryRouter initialPath="/series/tvdb:99999">
        <QueryClientProvider
          client={
            new QueryClient({
              defaultOptions: { queries: { retry: false, staleTime: Number.POSITIVE_INFINITY } },
            })
          }
        >
          <Story />
        </QueryClientProvider>
      </StoryRouter>
    ),
  ],
  render: () => (
    <div className="min-h-dvh bg-paper">
      <SeriesDetail seriesRef="tvdb:99999" />
    </div>
  ),
};

/** Error branch — pre-seeded with a thrown error so the retry-disabled
 *  client surfaces the failure path. */
export const ErrorState: Story = {
  decorators: [withErrorShow('tvdb:99999')],
  render: () => (
    <div className="min-h-dvh bg-paper">
      <SeriesDetail seriesRef="tvdb:99999" />
    </div>
  ),
};
