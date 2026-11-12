import { useEffect, useState } from 'react';
import { IconActivity, IconRefresh } from '@tabler/icons-react';
import { statsStore } from '../lib/stores/statsStore';
import { CheckConnection } from '../../wailsjs/go/backend/App';
import type { ConnectionCheck, Dashboard as DashboardData } from '../lib/types';
import StatTile from './ui/StatTile';
import styles from './Dashboard.module.css';

const STATE_LABEL: Record<string, string> = {
  idle: 'Отключено',
  starting: 'Запуск…',
  connecting: 'Подключение…',
  reconnecting: 'Переподключение…',
  connected: 'Подключено',
  degraded: 'Деградация',
  disconnected: 'Отключено',
};

function fmtSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec < 0) return '0 Б/с';
  const units = ['Б/с', 'КБ/с', 'МБ/с', 'ГБ/с'];
  let v = bytesPerSec;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

function fmtBytes(bytes: number): string {
  if (!bytes || bytes < 0) return '0 Б';
  const units = ['Б', 'КБ', 'МБ', 'ГБ'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

function fmtUptime(sec: number): string {
  if (!sec || sec < 0) return '—';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  if (h > 0) return `${h}ч ${m}м`;
  if (m > 0) return `${m}м ${s}с`;
  return `${s}с`;
}

function fmtRtt(ms: number): string {
  if (!ms || ms <= 0) return '—';
  return `${ms < 10 ? ms.toFixed(1) : Math.round(ms)} мс`;
}

// Dashboard — live-панель состояния туннеля (U1). Данные приходят из бэкенда
// событием "stats" раз в секунду; кнопка проверки — активный DNS-запрос.
export default function Dashboard() {
  const [data, setData] = useState<DashboardData | null>(() => statsStore.get());
  const [checking, setChecking] = useState(false);
  const [check, setCheck] = useState<ConnectionCheck | null>(null);

  useEffect(() => statsStore.subscribe(setData), []);

  const state = data?.state ?? 'idle';
  const stateLabel = STATE_LABEL[state] ?? state;
  const healthy = state === 'connected';

  const runCheck = async () => {
    setChecking(true);
    setCheck(null);
    try {
      const res = await CheckConnection();
      setCheck(res);
    } catch (err) {
      setCheck({
        ok: false, workersAlive: 0, latencyMs: 0, resolved: 0,
        error: String(err ?? 'неизвестная ошибка'), checkedAt: Date.now() / 1000,
      });
    } finally {
      setChecking(false);
    }
  };

  return (
    <section className={styles.card} aria-labelledby="dashboard-title">
      <header className={styles.head}>
        <IconActivity size={18} stroke={2} className={styles.icon} aria-hidden="true" />
        <h2 id="dashboard-title" className={styles.title}>Состояние</h2>
        <span
          className={`${styles.badge} ${healthy ? styles.badgeOk : state === 'degraded' ? styles.badgeWarn : styles.badgeIdle}`}
          role="status"
          aria-live="polite"
        >
          {stateLabel}
        </span>
      </header>

      <div className={styles.grid}>
        <StatTile
          label="Приём"
          value={fmtSpeed(data?.speedRx ?? 0)}
          sub={`всего ${fmtBytes(data?.totalRx ?? 0)}`}
          tone="accent"
        />
        <StatTile
          label="Отдача"
          value={fmtSpeed(data?.speedTx ?? 0)}
          sub={`всего ${fmtBytes(data?.totalTx ?? 0)}`}
        />
        <StatTile
          label="Воркеры"
          value={`${data?.workersAlive ?? 0} / ${data?.workersTotal ?? 0}`}
          sub="живых / всего"
          tone={data && data.workersAlive > 0 ? 'default' : 'danger'}
        />
        <StatTile
          label="RTT"
          value={fmtRtt(data?.avgRttMs ?? 0)}
          sub="средний по воркерам"
        />
        <StatTile
          label="Аптайм"
          value={fmtUptime(data?.uptimeSec ?? 0)}
          sub="текущая сессия"
        />
        <StatTile
          label="Реконнекты"
          value={String(data?.reconnects ?? 0)}
          sub="с запуска"
        />
      </div>

      {(data?.dnsResponses ?? 0) > 0 && (
        <p className={styles.meta}>
          DNS: {data?.dnsResponses ?? 0} ответов
          {(data?.dnsFailures ?? 0) > 0 ? `, ${data?.dnsFailures} ошибок` : ''}
        </p>
      )}

      {data?.lastError ? (
        <p className={styles.error} role="alert">
          Последняя ошибка: {data.lastError}
        </p>
      ) : null}

      <div className={styles.actions}>
        <button
          type="button"
          className={styles.checkBtn}
          onClick={runCheck}
          disabled={checking}
          aria-busy={checking}
        >
          <IconRefresh size={15} stroke={2} aria-hidden="true" />
          {checking ? 'Проверяем…' : 'Проверить соединение'}
        </button>
        <span
          className={`${styles.checkResult} ${check ? (check.ok ? styles.ok : styles.fail) : ''}`}
          role="status"
          aria-live="polite"
        >
          {check
            ? check.ok
              ? `OK · ${check.resolved} IP · ${fmtRtt(check.latencyMs)}`
              : `Ошибка: ${check.error || 'нет ответа'}`
            : ''}
        </span>
      </div>
    </section>
  );
}
