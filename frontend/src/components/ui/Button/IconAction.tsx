import type { IconComponent } from '../../../types';
import { Icon } from '../Icon/Icon';
import { Button } from './Button';

/** A small icon-only button with its name as tooltip. */
export function IconAction({
  icon,
  label,
  onClick,
  variant = 'ghost',
  disabled,
}: {
  icon: IconComponent;
  label: string;
  onClick: () => void;
  variant?: 'ghost' | 'primary' | 'danger' | 'secondary';
  disabled?: boolean;
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={variant}
      className="btn-icon"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      <Icon icon={icon} size="sm" />
    </Button>
  );
}
