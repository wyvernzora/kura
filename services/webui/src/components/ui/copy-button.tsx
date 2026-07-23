import { useEffect, useRef, useState } from 'react';

import { GhostIconButton } from '@/components/ui/ghost-icon-btn';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';

type CopyState = 'idle' | 'copied' | 'failed';

interface CopyButtonProps {
  text: string;
  what?: string;
  label: string;
  icon?: string;
  variant?: 'pill' | 'icon';
  className?: string;
}

export function CopyButton({
  text,
  what,
  label,
  icon = 'content_copy',
  variant = 'pill',
  className,
}: CopyButtonProps) {
  const [state, setState] = useState<CopyState>('idle');
  const [announcement, setAnnouncement] = useState('');
  const stateTimeout = useRef<ReturnType<typeof setTimeout>>(undefined);
  const announcementTimeout = useRef<ReturnType<typeof setTimeout>>(undefined);
  const attempt = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      attempt.current += 1;
      clearTimeout(stateTimeout.current);
      clearTimeout(announcementTimeout.current);
    };
  }, []);

  const copy = async () => {
    const currentAttempt = ++attempt.current;
    clearTimeout(stateTimeout.current);
    clearTimeout(announcementTimeout.current);
    setState('idle');
    setAnnouncement('');

    const copied = await copyText(text);
    if (!mounted.current || currentAttempt !== attempt.current) {
      return;
    }

    const nextState = copied ? 'copied' : 'failed';
    const message = copied ? `Copied ${what || 'to clipboard'}` : 'Copy failed';
    setState(nextState);
    announcementTimeout.current = setTimeout(() => setAnnouncement(message), 30);
    stateTimeout.current = setTimeout(() => setState('idle'), 1600);
  };

  const accessibleLabel =
    state === 'copied'
      ? `Copied ${what || 'to clipboard'}`
      : state === 'failed'
        ? 'Copy failed'
        : label;
  const title = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label;
  const stateIcon = state === 'copied' ? 'check' : state === 'failed' ? 'error' : icon;

  if (variant === 'icon') {
    return (
      <GhostIconButton
        size="lg"
        onClick={copy}
        aria-label={accessibleLabel}
        title={title}
        className={cn(
          'shrink-0',
          state === 'copied' && 'text-status-complete',
          state === 'failed' && 'text-status-error',
          className,
        )}
      >
        <MaterialIcon name={stateIcon} size={16} />
        <span className="sr-only" aria-live="polite">
          {announcement}
        </span>
      </GhostIconButton>
    );
  }

  return (
    <button
      type="button"
      onClick={copy}
      aria-label={accessibleLabel}
      title={title}
      className={cn(
        'inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border px-2.5 font-mono text-[11px] font-medium transition-colors',
        state === 'copied'
          ? 'border-status-complete/40 bg-status-complete/10 text-status-complete'
          : state === 'failed'
            ? 'border-status-error/40 bg-status-error/10 text-status-error'
            : 'border-line-soft bg-surface-2 text-ink-2 hover:bg-overlay-soft hover:text-ink',
        className,
      )}
    >
      <MaterialIcon name={stateIcon} size={14} />
      <span>{state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label}</span>
      <span className="sr-only" aria-live="polite">
        {announcement}
      </span>
    </button>
  );
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (typeof navigator !== 'undefined' && typeof navigator.clipboard?.writeText === 'function') {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through to the legacy copy path.
  }
  return legacyCopy(text);
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined' || !document.body) {
    return false;
  }

  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.setAttribute('aria-hidden', 'true');
  textarea.tabIndex = -1;
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  textarea.style.opacity = '0';
  textarea.style.pointerEvents = 'none';

  try {
    document.body.appendChild(textarea);
    textarea.select();
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    textarea.remove();
  }
}
