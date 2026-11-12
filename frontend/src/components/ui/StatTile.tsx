import styles from './StatTile.module.css';

export type StatTone = 'default' | 'accent' | 'danger' | 'muted';

interface StatTileProps {
  label: string;
  value: string;
  sub?: string;
  tone?: StatTone;
}

// StatTile — общий примитив для метрик дашборда (U7/U12).
export default function StatTile({ label, value, sub, tone = 'default' }: StatTileProps) {
  return (
    <div className={`${styles.tile} ${styles[tone]}`}>
      <span className={styles.label}>{label}</span>
      <span className={styles.value}>{value}</span>
      {sub && <span className={styles.sub}>{sub}</span>}
    </div>
  );
}
