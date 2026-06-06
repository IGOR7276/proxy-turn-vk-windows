import { useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Tunnel from './pages/Tunnel';
import Deploy from './pages/Deploy';
import Logs from './pages/Logs';
import Info from './pages/Info';
import SettingsPage from './pages/Settings';
import Toast from './components/Toast';
import { wdttLinkStore, parseWdttUrl } from './lib/utils/wdttLink';
import { toastStore } from './lib/stores/toastStore';
import { logStore } from './lib/stores/logStore';
import { tunnelStore } from './lib/stores/tunnelStore';
import type { LogLevel } from './lib/stores/logStore';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { settingsStore } from './lib/store';
import { SetTrayEnabled } from '../wailsjs/go/backend/App';

function useWdttPaste() {
  useEffect(() => {
    const handler = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData('text') ?? '';
      if (!text.trim().startsWith('wdtt://')) return;
      const tag = (document.activeElement as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      e.preventDefault();
      const link = parseWdttUrl(text.trim());
      if (!link) { toastStore.show('Неверный формат ссылки'); return; }
      wdttLinkStore.set(link);
    };
    document.addEventListener('paste', handler);
    document.body.tabIndex = 0;
    return () => document.removeEventListener('paste', handler);
  }, []);
}

function useWailsEvents() {
  useEffect(() => {
    const offs = [
      EventsOn('log', (level: unknown, msg: unknown) => {
        logStore.push((level as LogLevel) ?? 'INFO', String(msg ?? ''));
      }),
      EventsOn('error', (msg: unknown) => {
        logStore.push('ERROR', String(msg ?? ''));
      }),
      EventsOn('state_changed', (status: unknown) => {
        const s = String(status ?? '');
        if (s === 'running') { tunnelStore.set('connected'); logStore.push('INFO', '✓ Туннель активен'); }
        else if (s === 'connecting') { tunnelStore.set('connecting'); logStore.push('INFO', '⟳ Подключение...'); }
        else if (s === 'stopped' || s === 'error' || s === 'disconnected') { tunnelStore.set('idle'); logStore.push('INFO', '— Отключено'); }
      }),
      EventsOn('event', (name: unknown) => {
        if (name === 'wg_config') tunnelStore.set('connected');
      }),
    ];
    return () => offs.forEach(off => off());
  }, []);
}

export default function App() {
  useWailsEvents();
  useWdttPaste();

  useEffect(() => {
    const s = settingsStore.get();
    SetTrayEnabled(s.tray);
  }, []);

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Tunnel />} />
          <Route path="/deploy" element={<Deploy />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/info" element={<Info />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Route>
      </Routes>
      <Toast />
    </BrowserRouter>
  );
}
