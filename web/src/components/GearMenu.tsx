import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { IconBtn } from '@/components/ui/icon-btn';
import { MaterialIcon } from '@/components/ui/material-icon';
import { type Theme, useTheme } from '@/state/theme';

const THEME_OPTIONS: ReadonlyArray<{ value: Theme; label: string; icon: string }> = [
  { value: 'light', label: 'Light', icon: 'light_mode' },
  { value: 'dark', label: 'Dark', icon: 'dark_mode' },
  { value: 'system', label: 'System', icon: 'brightness_auto' },
];

/**
 * Gear button in the top bar. Replaces the previous standalone
 * ThemeToggle: the gear opens a dropdown with the appearance group
 * (light / dark / system) so future server-admin actions can land in
 * the same menu without crowding the chrome.
 *
 * All icons come from Material Symbols Outlined (loaded via
 * `index.html`) for visual consistency with the rest of the chrome.
 */
export function GearMenu() {
  const theme = useTheme((s) => s.theme);
  const setTheme = useTheme((s) => s.setTheme);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <IconBtn aria-label="Settings">
          <MaterialIcon name="settings" size={18} />
        </IconBtn>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[12rem]">
        <DropdownMenuLabel>Appearance</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme} onValueChange={(value) => setTheme(value as Theme)}>
          {THEME_OPTIONS.map((opt) => (
            <DropdownMenuRadioItem key={opt.value} value={opt.value}>
              <span className="flex items-center gap-2">
                <MaterialIcon name={opt.icon} size={16} />
                {opt.label}
              </span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
