import type { Meta, StoryObj } from '@storybook/react';

import { ClearFiltersButton } from './ClearFiltersButton';

const meta: Meta<typeof ClearFiltersButton> = {
  title: 'Library/ClearFiltersButton',
  component: ClearFiltersButton,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof ClearFiltersButton>;

export const Default: Story = {
  args: { onClick: () => {} },
};

/** Two viewports demonstrate the responsive collapse: the icon-only
 *  variant under lg, the text+icon variant at lg+. */
export const Responsive: Story = {
  render: () => (
    <div className="flex flex-col gap-6 text-sm text-muted">
      <div className="flex items-center gap-3">
        <span className="font-mono text-[10px] uppercase tracking-wide">≥ lg</span>
        <ClearFiltersButton onClick={() => {}} />
      </div>
      <div className="flex items-center gap-3">
        <span className="font-mono text-[10px] uppercase tracking-wide">&lt; lg</span>
        {/* Force the icon-only branch by capping the wrapper width below
         *  the lg breakpoint. */}
        <div className="w-[180px]">
          <ClearFiltersButton onClick={() => {}} />
        </div>
      </div>
    </div>
  ),
};
