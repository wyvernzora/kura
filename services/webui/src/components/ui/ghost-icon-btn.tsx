import { type ButtonHTMLAttributes, forwardRef } from 'react';

import { cn } from '@/lib/cn';

interface GhostIconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  size: 'sm' | 'md' | 'lg';
}

const SIZE_CLASS = {
  sm: 'h-7 w-7',
  md: 'h-8 w-8',
  lg: 'h-9 w-9',
} as const;

export const GhostIconButton = forwardRef<HTMLButtonElement, GhostIconButtonProps>(
  ({ size, className, type, ...props }, ref) => (
    <button
      ref={ref}
      type={type ?? 'button'}
      className={cn(
        'inline-flex cursor-pointer items-center justify-center rounded-md text-muted transition-colors hover:bg-overlay-soft hover:text-ink',
        SIZE_CLASS[size],
        className,
      )}
      {...props}
    />
  ),
);
GhostIconButton.displayName = 'GhostIconButton';
