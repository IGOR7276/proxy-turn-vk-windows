import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Tunnel from './pages/Tunnel';
import Deploy from './pages/Deploy';
import Logs from './pages/Logs';
import Info from './pages/Info';
import SettingsPage from './pages/Settings';
import Exclusions from './pages/Exclusions';
import Toast from './components/Toast';
import CloseDialog from './modals/CloseDialog';
import CaptchaSolve from './modals/CaptchaSolve';
import { wdttLinkStore, parseWdttUrl } from './lib/utils/wdttLink';
import { toastStore } from './lib/stores/toastStore';
import { logStore } from './lib/stores/logStore';
import { tunnelStore } from './lib/stores/tunnelStore';
import { pipelineStore } from './lib/stores/pipelineStore';
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
        else if (s === 'reconnecting') { tunnelStore.set('reconnecting'); logStore.push('WARN', '⟳ Обрыв — переподключение...'); pipelineStore.hide(); }
        else if (s === 'stopped' || s === 'error' || s === 'disconnected') { tunnelStore.set('idle'); logStore.push('INFO', '— Отключено'); pipelineStore.hide(); }
      }),
      EventsOn('pipeline_state', (payload: unknown) => {
        const p = payload && typeof payload === 'object' ? payload : {};
        pipelineStore.setFromBackend({
          visible: Boolean((p as any).visible),
          current: (p as any).current ?? null,
          completed: Array.isArray((p as any).completed) ? (p as any).completed : [],
          failed: (p as any).failed ?? null,
          timedOut: Boolean((p as any).timedOut),
          timeoutSec: Number((p as any).timeoutSec ?? 0),
        });
      }),
      EventsOn('event', (name: unknown) => {
        if (name === 'wg_config') tunnelStore.set('connected');
        if (name === 'pipeline_start') pipelineStore.reset();
      }),
      EventsOn('vk_auth_required', (hash: unknown) => {
        logStore.push('WARN', `Требуется вход через VK для хеша: ${String(hash ?? '')}`);
        window.dispatchEvent(new CustomEvent('vk_auth_required', { detail: String(hash ?? '') }));
      }),
      EventsOn('captcha_required', (data: unknown) => {
        const str = String(data ?? '');
        logStore.push('WARN', 'Требуется решение капчи');
        // Извлекаем redirectURI из data (формат: mode|redirectURI|sessionToken)
        const parts = str.split('|');
        if (parts.length >= 2) {
          window.open(parts[1], '_blank');
        }
        window.dispatchEvent(new CustomEvent('captcha_required', { detail: str }));
      }),
    ];
    return () => offs.forEach(off => off());
  }, []);
}

export default function App() {
  useWailsEvents();
  useWdttPaste();
  const [closeDialog, setCloseDialog] = useState(false);
  const [showCaptcha, setShowCaptcha] = useState(false);
  const [updateInfo, setUpdateInfo] = useState<{ version: string; url: string } | null>(null);

  useEffect(() => {
    const s = settingsStore.get();
    SetTrayEnabled(s.tray);
    SetCloseActionPreference(s.closeAction);
  }, []);

  useEffect(() => {
    const off1 = EventsOn('show_close_dialog', () => setCloseDialog(true));
    const off2 = EventsOn('captcha_required', () => setShowCaptcha(true));
    const off3 = EventsOn('update_available', (version: unknown, url: unknown) => {
      setUpdateInfo({ version: String(version ?? ''), url: String(url ?? '') });
    });
    return () => { off1(); off2(); off3(); };
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
          <Route path="/exclusions" element={<Exclusions />} />
        </Route>
      </Routes>
      <Toast />
      {closeDialog && (
        <CloseDialog
          onClose={() => setCloseDialog(false)}
          onChoose={handleCloseChoice}
        />
      )}
      {showCaptcha && (
        <CaptchaSolve onClose={() => setShowCaptcha(false)} />
      )}
      {updateInfo && (
        <div style={{
          position: 'fixed', inset: 0, zIndex: 9999,
          background: 'rgba(0,0,0,0.5)', display: 'flex',
          alignItems: 'center', justifyContent: 'center',
        }} onClick={() => setUpdateInfo(null)}>
          <div style={{
            background: 'var(--surface)', borderRadius: 'var(--r-card)',
            padding: 24, maxWidth: 400, width: '90%',
            boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
            border: '1px solid var(--border)',
          }} onClick={e => e.stopPropagation()}>
            <h3 style={{ margin: '0 0 8px', color: 'var(--text)' }}>Доступно обновление</h3>
            <p style={{ margin: '0 0 16px', color: 'var(--text-2)', fontSize: 14 }}>
              Версия <strong>{updateInfo.version}</strong> готова к установке.
            </p>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button className="sp-seg-btn" onClick={() => setUpdateInfo(null)}>Позже</button>
              <button className="sp-seg-btn sp-seg-btn--active" onClick={() => {
                window.open(updateInfo.url, '_blank');
                setUpdateInfo(null);
              }}>Скачать</button>
            </div>
          </div>
        </div>
      )}
    </BrowserRouter>
  );
}

