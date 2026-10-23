import React from 'react';
import './Spinner.css';

export interface SpinnerProps {
  className?: string;
  size?: number;
}

export const Spinner: React.FC<SpinnerProps> = ({ className = '', size = 20 }) => {
  return (
    <span className={`spinner ${className}`} style={{ width: `${size}px`, height: `${size}px` }} />
  );
};
