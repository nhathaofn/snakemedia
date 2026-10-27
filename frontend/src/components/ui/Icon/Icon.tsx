import './Icon.css';
import type { IconComponent } from '../../../types';

export type IconSize = 'sm' | 'md' | 'lg' | 'xl' | 'display';

export interface IconProps {
  icon: IconComponent;
  size?: IconSize;
  className?: string;
}

/**
 * Renders a monochrome line icon. Colour follows `currentColor`; size and stroke
 * come from tokens.css (--icon-size-*, --icon-stroke). Always decorative: give the
 * surrounding control an accessible label.
 */
export function Icon({ icon: Glyph, size = 'md', className = '' }: IconProps) {
  return (
    <Glyph className={`icon icon-${size} ${className}`} aria-hidden="true" focusable="false" />
  );
}
