import { useState, useEffect } from 'react';
import { IconInfoCircle, IconBrandGithub, IconCopy, IconCheck, IconBolt, IconUsers, IconServer, IconClock, IconHeart, IconExternalLink, IconShield, IconWorld } from '@tabler/icons-react';
import iconUrl from '../assets/icon.png';
import { logStore } from '../lib/stores/logStore';
import { settingsStore } from '../lib/store';
import { themeStore } from '../lib/stores/themeStore';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { IsRunning } from '../../wailsjs/go/backend/App';


const VERSION = '2.0.2';
const BUILD_DATE = '07.06.2026';
const REPO_URL = 'https://github.com/IGOR7276/proxy-turn-vk-windows';

export default function Info() {
  const [copied, setCopied] = useState<string | null>(null);
  const [stats, setStats] = useState({ logs: 0, hashes: 0, power: 0, mtu: 0, dnsEnabled: true });
  const [running, setRunning] = useState(false);
  const [theme, setTheme] = useState(() => themeStore.get());
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());

  useEffect(() => themeStore.subscribe(setTheme), []);
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  useEffect(() => {
    const update = () => {
      const s = settingsStore.get();
      setStats({
        logs: logStore.getAll().length,
        hashes: s.hashes.filter(h => h.trim()).length,
        power: s.power,
        mtu: s.mtu,
        dnsEnabled: s.dnsProxyEnabled,
      });
    };
    update();
    const i = setInterval(update, 1000);
    IsRunning().then(v => setRunning(v));
    return () => clearInterval(i);
  }, []);

  const copy = (key: string, value: string) => {
    navigator.clipboard.writeText(value);
    setCopied(key);
    setTimeout(() => setCopied(null), 1200);
  };

  return (
    <>
      <style>{`
        .if-wrap { display: flex; flex-direction: column; gap: 18px; animation: page-in 0.3s ease-out; }
        .if-header { display: flex; align-items: center; gap: 10px; padding: 4px 4px 0; }
        .if-title { font-size: 20px; font-weight: 600; color: var(--text); flex: 1; }
        .if-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--r-card); padding: 22px 24px; box-shadow: var(--shadow); }
        .if-hero { text-align: center; padding: 32px 24px 24px; }
        .if-logo { width: 88px; height: 88px; margin: 0 auto 14px; border-radius: 22px; display: flex; align-items: center; justify-content: center; box-shadow: 0 8px 24px rgba(45, 74, 122, 0.15); overflow: hidden; }
        .if-logo img { width: 100%; height: 100%; display: block; object-fit: cover; }
        .if-logo-text { font-size: 32px; font-weight: 700; color: var(--accent-fg); }
        .if-appname { font-size: 24px; font-weight: 600; color: var(--text); margin-bottom: 4px; }
        .if-tagline { font-size: 13px; color: var(--text-3); }
        .if-version-pill { display: inline-block; padding: 4px 12px; background: var(--accent-soft); color: var(--accent); border-radius: var(--r-pill); font-size: 12px; font-weight: 600; margin-top: 10px; }
        .if-section-label { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.5px; font-weight: 600; padding: 4px 4px 0; }
        .if-row { display: flex; align-items: center; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid var(--border-2); gap: 12px; }
        .if-row:last-child { border-bottom: none; }
        .if-label { color: var(--text); font-size: 14px; display: flex; align-items: center; gap: 10px; }
        .if-label svg { color: var(--text-3); }
        .if-value { color: var(--text-2); font-size: 14px; font-family: 'Geist Mono', monospace; display: flex; align-items: center; gap: 6px; }
        .if-icon-btn { width: 28px; height: 28px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); cursor: pointer; display: flex; align-items: center; justify-content: center; color: var(--text-3); transition: background 0.12s, color 0.12s; padding: 0; flex-shrink: 0; }
        .if-icon-btn:hover { background: var(--bg-2); color: var(--text); }
        .if-icon-btn--ok { color: var(--success); }
        .if-stats { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; }
        .if-stat { background: var(--surface-2); border-radius: var(--r-input); padding: 14px 16px; text-align: center; }
        .if-stat-val { font-size: 22px; font-weight: 600; color: var(--accent); line-height: 1; }
        .if-stat-lbl { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.4px; margin-top: 4px; }
        .if-status-dot { width: 8px; height: 8px; border-radius: 50%; }
        .if-status-dot--on { background: var(--success); }
        .if-status-dot--off { background: var(--text-4); }
        .if-link { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 12px; border: 1.5px solid var(--border); border-radius: var(--r-input); background: var(--surface-2); color: var(--accent); text-decoration: none; font-family: 'Geist', sans-serif; font-size: 14px; font-weight: 600; margin: 4px 0; transition: background 0.15s; cursor: pointer; }
        .if-link:hover { background: var(--bg-2); }
        .if-credits { text-align: center; font-size: 12px; color: var(--text-3); padding: 12px 0; display: flex; align-items: center; justify-content: center; gap: 6px; }
      `}</style>

      <div className="if-wrap">
        <div className="if-header">
          <IconInfoCircle size={22} stroke={2} />
          <div className="if-title">Информация</div>
        </div>

        {/* Hero */}
        <div className="if-card if-hero">
          <div className="if-logo">
            <img src={iconUrl} alt="WDTT" />
          </div>
          <div className="if-appname">WDTT</div>
          <div className="if-tagline">WireGuard-DTLS-Туннель-Трафик</div>
          <div className="if-version-pill">v{VERSION} · {BUILD_DATE}</div>
        </div>

        {/* Session */}
        <div className="if-section-label">Текущая сессия</div>
        <div className="if-card">
          <div className="if-row">
            <span className="if-label">
              <IconBolt size={16} />
              Статус
            </span>
            <span className="if-value" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span className={`if-status-dot ${running ? 'if-status-dot--on' : 'if-status-dot--off'}`} />
              {running ? 'Активен' : tunnelState === 'connecting' ? 'Подключение' : 'Не запущен'}
            </span>
          </div>
          <div className="if-row">
            <span className="if-label">
              <IconUsers size={16} />
              Хешей VK
            </span>
            <span className="if-value">{stats.hashes}/4</span>
          </div>
          <div className="if-row">
            <span className="if-label">
              <IconBolt size={16} />
              Мощность
            </span>
            <span className="if-value">{stats.power}</span>
          </div>
          <div className="if-row">
            <span className="if-label">
              <IconServer size={16} />
              MTU
            </span>
            <span className="if-value">{stats.mtu}</span>
          </div>
          <div className="if-row">
            <span className="if-label">
              <IconWorld size={16} />
              DNS-прокси
            </span>
            <span className="if-value">{stats.dnsEnabled ? 'включён' : 'выключен'}</span>
          </div>
        </div>

        {/* Quick stats */}
        <div className="if-section-label">Статистика</div>
        <div className="if-stats">
          <div className="if-stat">
            <div className="if-stat-val">{stats.logs}</div>
            <div className="if-stat-lbl">Строк лога</div>
          </div>
          <div className="if-stat">
            <div className="if-stat-val">{theme === 'dark' ? 'Тьма' : 'Свет'}</div>
            <div className="if-stat-lbl">Тема</div>
          </div>
        </div>

        {/* Build info */}
        <div className="if-section-label">Сборка</div>
        <div className="if-card">
          <div className="if-row">
            <span className="if-label">
              <IconClock size={16} />
              Версия
            </span>
            <span className="if-value">
              v{VERSION}
              <button
                className={`if-icon-btn ${copied === 'v' ? 'if-icon-btn--ok' : ''}`}
                onClick={() => copy('v', VERSION)}
                title="Копировать"
              >
                {copied === 'v' ? <IconCheck size={13} /> : <IconCopy size={13} stroke={2} />}
              </button>
            </span>
          </div>
          <div className="if-row">
            <span className="if-label">
              <IconShield size={16} />
              Платформа
            </span>
            <span className="if-value">windows/amd64</span>
          </div>
          <div className="if-row">
            <span className="if-label">
              <IconClock size={16} />
              Дата сборки
            </span>
            <span className="if-value">{BUILD_DATE}</span>
          </div>
        </div>

        {/* Links */}
        <div className="if-section-label">Ресурсы</div>
        <div className="if-card">
          <a className="if-link" href={REPO_URL} target="_blank" rel="noopener">
            <IconBrandGithub size={16} />
            GitHub репозиторий
            <IconExternalLink size={14} />
          </a>
        </div>

        <div className="if-credits">
          <IconHeart size={14} />
          сделано с заботой для тех, кто верит в свободу интернета
        </div>
      </div>
    </>
  );
}
