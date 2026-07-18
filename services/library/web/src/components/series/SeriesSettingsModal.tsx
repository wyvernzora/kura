import * as DialogPrimitive from '@radix-ui/react-dialog';

import { apiErrorMessage } from '@/api/client';
import { useUpdateTags } from '@/api/hooks';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import {
  computeTagExpressions,
  maintenanceRequested,
  PRIORITY_TAG,
  type Priority,
  priorityFromTags,
} from '@/lib/seriesSettings';

interface SeriesSettingsModalProps {
  metadataRef: string;
  tags: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

interface PriorityOption {
  id: Priority;
  tag: string | null;
  label: string;
  icon: string;
  hint: string;
}

const PRIORITY = {
  label: 'Priority',
  icon: 'flag',
  help: 'How important this series is to you. External workflows may use this setting to customize how they handle this series.',
  options: [
    {
      id: 'high',
      tag: PRIORITY_TAG.high,
      label: 'High',
      icon: 'stat_2',
      hint: 'episodes sooner; policies can bend',
    },
    {
      id: 'normal',
      tag: null,
      label: 'Normal',
      icon: 'stat_0',
      hint: 'default handling',
    },
    {
      id: 'low',
      tag: PRIORITY_TAG.low,
      label: 'Low',
      icon: 'stat_minus_2',
      hint: 'keep tracking, don’t notify',
    },
    {
      id: 'disabled',
      tag: PRIORITY_TAG.disabled,
      label: 'Disabled',
      icon: 'do_not_disturb_on',
      hint: 'not maintained — workflows skip it',
    },
  ] satisfies PriorityOption[],
};

const MAINTENANCE = {
  label: 'Maintenance',
  icon: 'build',
  help: 'External workflows may use this setting to run one-off maintenance on this series. Cleared automatically once maintenance completes.',
};

export function SeriesSettingsModal({
  metadataRef,
  tags,
  open,
  onOpenChange,
}: SeriesSettingsModalProps) {
  const updateTags = useUpdateTags(metadataRef);
  const priority = priorityFromTags(tags);
  const requested = maintenanceRequested(tags);
  const disabled = priority === 'disabled';
  const requestedActive = requested && !disabled;

  const applyPriority = (nextPriority: Priority) => {
    const expressions = computeTagExpressions(tags, nextPriority, requestedActive);
    if (expressions.length > 0) {
      updateTags.mutate(expressions);
    }
  };

  const applyRequested = (nextRequested: boolean) => {
    const expressions = computeTagExpressions(tags, priority, nextRequested);
    if (expressions.length > 0) {
      updateTags.mutate(expressions);
    }
  };

  const errorMessage = updateTags.isError
    ? apiErrorMessage(updateTags.error, 'Failed to update settings')
    : undefined;

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[70] bg-overlay backdrop-blur-[2px] data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <div className="pointer-events-none fixed inset-0 z-[71] flex items-end justify-center sm:items-center sm:p-6">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className={cn(
              'pointer-events-auto relative max-h-[88dvh] w-full overflow-y-auto',
              'rounded-t-[16px] bg-surface text-ink shadow-pop',
              'sm:w-[520px] sm:max-w-[92vw] sm:rounded-[14px]',
            )}
          >
            <div className="flex justify-center pt-2 sm:hidden">
              <span className="h-1 w-9 rounded-full bg-line" />
            </div>
            <div className="flex flex-col">
              <div className="flex items-center gap-2 border-line-soft border-b px-[18px] py-[14px]">
                <MaterialIcon name="tune" size={16} className="text-muted" />
                <DialogPrimitive.Title className="text-[15px] font-semibold text-ink">
                  Settings
                </DialogPrimitive.Title>
                <DialogPrimitive.Close
                  type="button"
                  aria-label="Close"
                  className="ml-auto inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-muted transition-colors hover:bg-overlay-soft hover:text-ink"
                >
                  <MaterialIcon name="close" size={18} />
                </DialogPrimitive.Close>
              </div>
              <div
                className={cn(
                  'divide-y divide-line-soft transition-opacity',
                  updateTags.isPending && 'pointer-events-none opacity-50',
                )}
                aria-busy={updateTags.isPending}
              >
                <div className="flex flex-col gap-3 px-[18px] py-4">
                  <AxisHead icon={PRIORITY.icon} title={PRIORITY.label} help={PRIORITY.help} />
                  <Segmented
                    value={priority}
                    disabled={updateTags.isPending}
                    onChange={applyPriority}
                  />
                </div>
                <div className="flex flex-col gap-3 px-[18px] py-4">
                  <AxisHead
                    icon={MAINTENANCE.icon}
                    title={MAINTENANCE.label}
                    help={MAINTENANCE.help}
                  />
                  <ToggleRow
                    title="Request maintenance"
                    help={disabled ? 'Unavailable while maintenance is disabled.' : undefined}
                    checked={requestedActive}
                    disabled={disabled || updateTags.isPending}
                    onChange={applyRequested}
                  />
                </div>
              </div>
              {errorMessage && (
                <div role="alert" className="px-[18px] pb-3 text-[12px] text-status-error">
                  {errorMessage}
                </div>
              )}
            </div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function Segmented({
  value,
  disabled,
  onChange,
}: {
  value: Priority;
  disabled: boolean;
  onChange: (value: Priority) => void;
}) {
  return (
    <div
      className="inline-flex w-full rounded-lg bg-line p-0.5"
      role="radiogroup"
      aria-label={PRIORITY.label}
    >
      {PRIORITY.options.map((option) => {
        const active = value === option.id;
        return (
          // biome-ignore lint/a11y/useSemanticElements: the reference requires segmented buttons with radio semantics.
          <button
            key={option.id}
            type="button"
            role="radio"
            aria-checked={active}
            title={option.hint}
            disabled={disabled}
            onClick={() => onChange(option.id)}
            className={cn(
              // radius-lg (14px) track minus 2px padding → 12px chip keeps the arcs concentric
              'inline-flex h-[30px] flex-1 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-[12px] px-3 text-[12px] font-medium transition-colors',
              active ? 'bg-surface text-ink shadow-card' : 'text-muted hover:text-ink',
              disabled && 'cursor-default',
            )}
          >
            <span className="hidden min-[390px]:inline-flex">
              <MaterialIcon
                name={option.icon}
                size={16}
                className={active ? 'text-ink' : 'text-muted'}
              />
            </span>
            {option.label}
          </button>
        );
      })}
    </div>
  );
}

function Toggle({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-label={label}
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-[22px] w-[38px] shrink-0 items-center rounded-full transition-colors',
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
        // off-track must stay visible on the bg-line row behind it
        checked ? 'bg-status-airing' : 'bg-muted/50',
      )}
    >
      <span
        className={cn(
          'inline-block h-[18px] w-[18px] rounded-full bg-surface shadow-card transition-transform',
          checked ? 'translate-x-[18px]' : 'translate-x-0.5',
        )}
      />
    </button>
  );
}

function AxisHead({ icon, title, help }: { icon: string; title: string; help: string }) {
  return (
    <div className="flex items-start gap-2.5">
      <MaterialIcon name={icon} size={16} className="mt-0.5 text-muted" />
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-ink">{title}</div>
        <div className="mt-0.5 text-[12px] text-muted">{help}</div>
      </div>
    </div>
  );
}

function ToggleRow({
  title,
  help,
  checked,
  disabled,
  onChange,
}: {
  title: string;
  help?: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg bg-line px-3 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="text-[13px] font-medium text-ink">{title}</div>
        {help && <div className="mt-0.5 text-[12px] text-muted">{help}</div>}
      </div>
      <Toggle label={title} checked={checked} disabled={disabled} onChange={onChange} />
    </div>
  );
}
