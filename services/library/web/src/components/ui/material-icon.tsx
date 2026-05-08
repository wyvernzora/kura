import { cn } from '@/lib/cn';

interface MaterialIconProps {
  /** Glyph name from https://fonts.google.com/icons (e.g. `flag`). */
  name: string;
  /** Optical font-size in px. Material Symbols renders crispest at the
   *  axis stops (20, 24, 40, 48); 18–20 are practical for inline UI. */
  size?: number;
  /** Forwarded to the wrapping <span>. */
  className?: string;
  /** Defaults to true: this is decoration paired with adjacent text. */
  ariaHidden?: boolean;
}

/**
 * Renders a single Material Symbols Outlined glyph. The font ships
 * via the `<link>` in `index.html` so consumers don't need to import
 * anything — just pass the glyph name.
 *
 * Layout: inline-block with a fixed line-height so the glyph sits on
 * the text baseline of pill-style buttons. Color inherits from the
 * surrounding `text-*` token.
 */
export function MaterialIcon({ name, size = 18, className, ariaHidden = true }: MaterialIconProps) {
  return (
    <span
      aria-hidden={ariaHidden}
      className={cn('material-symbols-outlined inline-block leading-none', className)}
      style={{
        fontSize: size,
        // Match the rendered glyph box to its optical size so
        // surrounding flex layout doesn't ride high or low.
        width: size,
        height: size,
        lineHeight: `${size}px`,
      }}
    >
      {name}
    </span>
  );
}
