import type { Meta, StoryObj } from '@storybook/react';

import { TopBar } from './TopBar';
import { StoryRouter } from './_storyRouter';

const meta: Meta<typeof TopBar> = {
  title: 'Chrome/TopBar',
  component: TopBar,
  parameters: {
    layout: 'fullscreen',
  },
};

// TopBar reads the current route via useRouterState and renders a
// back-to-library link on detail routes; every story wraps in the
// shared memory router so those primitives have a context. Per-story
// initialPath drives the detail-mode variant.
function withRouter(initialPath: string) {
  return function Decorator(Story: () => JSX.Element) {
    return (
      <StoryRouter initialPath={initialPath}>
        <Story />
      </StoryRouter>
    );
  };
}

export default meta;

type Story = StoryObj<typeof TopBar>;

/**
 * Top of the page — TopBar is invisible chrome: paper background, no
 * shadow, no border. Used for the at-rest visual.
 */
export const AtRest: Story = {
  args: { forceScrolled: false },
  decorators: [withRouter('/')],
  render: (args) => (
    <div className="min-h-[460px] bg-paper">
      <TopBar {...args} />
      <main className="mx-auto max-w-3xl px-6 py-6 text-sm text-muted">
        Page content sits beneath the bar. At scrollY === 0 the bar reads as part of the page — same
        paper tone, no separator. Scroll the page (or flip the Scrolled story) to see the levitation
        kick in.
      </main>
    </div>
  ),
};

/**
 * Forced scrolled state — translucent paper background with backdrop
 * blur + saturation and a soft drop shadow underneath. This is the
 * visual the bar acquires once the page scrolls past the first few
 * pixels.
 */
export const Scrolled: Story = {
  args: { forceScrolled: true },
  decorators: [withRouter('/')],
  render: (args) => (
    <div className="min-h-[460px] bg-paper">
      <TopBar {...args} />
      <main className="mx-auto max-w-3xl px-6 py-6 text-sm text-muted">
        Forced scrolled state for static review. The bar tints with translucent paper, backdrop
        blurs the content beneath, and drops a soft shadow at its base.
      </main>
    </div>
  ),
};

/**
 * Forced focus-within on the search field — drives the kura-focusable
 * focus glow into view without keyboard interaction.
 */
export const SearchFocused: Story = {
  args: { forceScrolled: false },
  parameters: { pseudo: { focusWithin: true } },
  decorators: [withRouter('/')],
  render: (args) => (
    <div className="min-h-[460px] bg-paper">
      <TopBar {...args} />
      <main className="mx-auto max-w-3xl px-6 py-6 text-sm text-muted">
        focus-within forced on every kura-focusable element so the search field's blue glow renders
        for visual review.
      </main>
    </div>
  ),
};

/**
 * Series detail variant — leading slot swaps the kura logo for the
 * "← Library" pill. Search, theme + gear stay put.
 */
export const DetailRoute: Story = {
  args: { forceScrolled: false },
  decorators: [withRouter('/series/tvdb:424536')],
  render: (args) => (
    <div className="min-h-[460px] bg-paper">
      <TopBar {...args} />
      <main className="mx-auto max-w-3xl px-6 py-6 text-sm text-muted">
        Detail-route chrome: logo replaced with the back-to-library pill; the rest of the bar is
        unchanged.
      </main>
    </div>
  ),
};
