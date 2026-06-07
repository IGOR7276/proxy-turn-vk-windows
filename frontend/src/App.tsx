import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Tunnel from './pages/Tunnel';
import Deploy from './pages/Deploy';
import Logs from './pages/Logs';
import Info from './pages/Info';
import SettingsPage from './pages/Settings';
import Toast from './components/Toast';
import CloseDialog from './modals/CloseDialog';
import { wdttLinkStore, parseWdttUrl } from './lib/utils/wdttLink';
import { toastStore } from './lib/stores/toastStore';
import { logStore } from './lib/stores/logStore';
import { tunnelStore } from './lib/stores/tunnelStore';
import type { LogLevel } from './lib/stores/logStore';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { settingsStore } from './lib/store';
import { SetTrayEnabled, SetCloseAction, SetCloseActionPreference } from '../wailsjs/go/backend/App';

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
  const [closeDialog, setCloseDialog] = useState(false);

  useEffect(() => {
    const s = settingsStore.get();
    SetTrayEnabled(s.tray);
    SetCloseActionPreference(s.closeAction);
  }, []);

  useEffect(() => {
    const off = EventsOn('show_close_dialog', () => setCloseDialog(true));
    return () => off();
  }, []);

  const handleCloseChoice = (action: 'hide' | 'exit', remember: boolean) => {
    setCloseDialog(false);
    if (remember) {
      const s = settingsStore.get();
      const next = { ...s, closeAction: action };
      settingsStore.save(next);
    }
    SetCloseAction(action, remember);
  };

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
      {closeDialog && (
        <CloseDialog
          onClose={() => setCloseDialog(false)}
          onChoose={handleCloseChoice}
        />
      )}
    </BrowserRouter>
  );
}

