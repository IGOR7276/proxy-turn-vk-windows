import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  IconPlugConnected,
  IconServer2,
  IconTerminal2,
  IconSettings2,
  IconInfoCircle,
  IconSun,
  IconMoon,
} from '@tabler/icons-react';
import { themeStore } from '../lib/stores/themeStore';

const TABS = [
  { path: '/', icon: IconPlugConnected, label: 'Туннель' },
  { path: '/deploy', icon: IconServer2, label: 'Деплой' },
  { path: '/logs', icon: IconTerminal2, label: 'Логи' },
  { path: '/settings', icon: IconSettings2, label: 'Настройки' },
  { path: '/info', icon: IconInfoCircle, label: 'Инфо' },
];

export default function BottomNav() {
  const navigate = useNavigate();
  const location = useLocation();
  const [theme, setTheme] = useState(() => themeStore.get());

  const toggleTheme = () => {
    themeStore.toggle();
    setTheme(themeStore.get());
  };

  return (
    <>
      <style>{`
        .bn-bar { position: fixed; bottom: 0; left: 0; right: 0; background: var(--surface); border-top: 1px solid var(--border); display: flex; align-items: stretch; justify-content: space-around; padding: 6px 8px calc(6px + env(safe-area-inset-bottom)); z-index: 50; }
        .bn-item { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; flex: 1; max-width: 90px; padding: 6px 4px; border: none; background: transparent; color: var(--text-3); font-family: 'Geist', sans-serif; font-size: 10px; font-weight: 600; cursor: pointer; border-radius: var(--r-nav-active); transition: color 0.15s, background 0.15s; }
        .bn-item:hover { color: var(--text); }
        .bn-item--active { color: var(--accent); background: var(--accent-soft); }
        .bn-item--active svg { color: var(--accent); }
        .bn-divider { width: 1px; background: var(--border); margin: 8px 4px; flex-shrink: 0; }
        .bn-theme { display: flex; align-items: center; justify-content: center; width: 44px; flex-shrink: 0; border: none; background: transparent; color: var(--text-3); cursor: pointer; border-radius: var(--r-nav-active); transition: color 0.15s, background 0.15s; }
        .bn-theme:hover { color: var(--text); background: var(--bg-2); }
      `}</style>
      <nav className="bn-bar">
        {TABS.map(({ path, icon: Icon, label }) => {
          const active = location.pathname === path;
          return (
            <button
              key={path}
              className={`bn-item${active ? ' bn-item--active' : ''}`}
              onClick={() => navigate(path)}
            >
              <Icon size={20} stroke={2} />
              <span>{label}</span>
            </button>
          );
        })}
        <div className="bn-divider" />
        <button className="bn-theme" onClick={toggleTheme} title="Сменить тему">
          {theme === 'light' ? <IconMoon size={20} stroke={2} /> : <IconSun size={20} stroke={2} />}
        </button>
      </nav>
    </>
  );
}
