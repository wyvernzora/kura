import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { StoryRouter } from '@/components/_storyRouter';
import { Sidebar } from '@/components/Sidebar';
import { useSidebar } from '@/state/sidebar';

/**
 * The rail/expanded split is persisted store state, so each story
 * seeds it before the component's first render. `className="flex"`
 * overrides the production `hidden md:flex` so the sidebar is visible
 * however narrow the Storybook canvas gets cropped.
 */
function Harness({ collapsed }: { collapsed: boolean }) {
  useState(() => {
    useSidebar.setState({ collapsed });
    return null;
  });
  return (
    <div className="flex h-[520px] bg-paper">
      <Sidebar className="flex" />
      <div className="flex-1 px-6 py-6 text-muted text-sm">
        Page column. Real app renders the top bar plus the route content here.
      </div>
    </div>
  );
}

const meta: Meta<typeof Sidebar> = {
  title: 'Chrome/Sidebar',
  component: Sidebar,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof Sidebar>;

/** Default resting shape — 62 px icon rail, Library active. */
export const Rail: Story = {
  render: () => (
    <StoryRouter initialPath="/">
      <Harness collapsed />
    </StoryRouter>
  ),
};

/** Expanded — 216 px with the wordmark and item labels. */
export const Expanded: Story = {
  render: () => (
    <StoryRouter initialPath="/">
      <Harness collapsed={false} />
    </StoryRouter>
  ),
};

/** Trash route — the active treatment moves to the second nav item. */
export const TrashActive: Story = {
  render: () => (
    <StoryRouter initialPath="/trash">
      <Harness collapsed={false} />
    </StoryRouter>
  ),
};

/** Settings route — the active treatment moves to the bottom item. */
export const SettingsActive: Story = {
  render: () => (
    <StoryRouter initialPath="/settings">
      <Harness collapsed={false} />
    </StoryRouter>
  ),
};

/** Detail route — `/series/$ref` still belongs to the library section. */
export const RailOnDetailRoute: Story = {
  render: () => (
    <StoryRouter initialPath="/series/tvdb:424536">
      <Harness collapsed />
    </StoryRouter>
  ),
};
