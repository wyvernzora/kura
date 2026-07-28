import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

/**
 * Conditionally compose class names and resolve conflicting Tailwind
 * utilities last-write-wins. Use everywhere a component accepts a
 * `className` prop so callers can override defaults predictably.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
