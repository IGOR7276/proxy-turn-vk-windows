import type { ButtonHTMLAttributes, ReactNode } from 'react';
import styles from './IconButton.module.css';

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** Текст для screen reader и подсказки. */
  label: string;
  children: ReactNode;
}

// IconButton — общий примитив для кнопок-иконок (U7/U12): всегда имеет
// доступное имя и видимый focus-ring.
export default function IconButton({ label, children, className, ...rest }: IconButtonProps) {
  return (
    <button
      type="button"
      className={`${styles.btn} ${className ?? ''}`}
      aria-label={label}
      title={label}
      {...rest}
    >
      {children}
    </button>
  );
}
