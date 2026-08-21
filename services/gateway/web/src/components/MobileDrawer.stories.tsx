import type { Meta, StoryObj } from '@storybook/react';

import { StoryRouter } from '@/components/_storyRouter';
import { MobileDrawer } from '@/components/MobileDrawer';

/**
 * The drawer is `md:hidden` in production; these stories pass
 * `md:block` so it renders on a desktop-width canvas. Closing is a
 * no-op here — the story is the open state.
 */
const meta: Meta<typeof MobileDrawer> = {
  title: 'Chrome/MobileDrawer',
  component: MobileDrawer,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof MobileDrawer>;

function Harness({ initialPath }: { initialPath: string }) {
  return (
    <StoryRouter initialPath={initialPath}>
      <div className="relative h-[520px] bg-paper px-6 py-6 text-muted text-sm">
        Page content behind the drawer scrim.
        <MobileDrawer open onClose={() => {}} className="md:block" />
      </div>
    </StoryRouter>
  );
}

/** Open over the library route — Library carries the active treatment. */
export const Open: Story = {
  render: () => <Harness initialPath="/" />,
};

/** Open over the settings route — the bottom item is active instead. */
export const OpenOnSettings: Story = {
  render: () => <Harness initialPath="/settings" />,
};
