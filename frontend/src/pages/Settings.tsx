import { useState, useEffect } from 'react';
import { IconSettings2, IconServer2, IconWorld, IconShield, IconSun, IconMoon, IconRotate } from '@tabler/icons-react';
import { settingsStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { themeStore } from '../lib/stores/themeStore';
import { toastStore } from '../lib/stores/toastStore';
import type { AppSettings } from '../lib/types';
import { SetTrayEnabled, SetAutoStart, GetAutoStart } from '../../wailsjs/go/backend/App';

export default function Settings() {
  const [settings, setSettings] = useState<AppSettings>(() => settingsStore.get());
  const [theme, setTheme] = useState(() => themeStore.get());
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());
  const [mtuRaw, setMtuRaw] = useState(String(settings.mtu || 1280));
  const [dnsUpstreamRaw, setDnsUpstreamRaw] = useState(settings.dnsUpstream);
  const [wgIface, setWgIface] = useState(settings.wgInterface || 'WDTT');

  useEffect(() => tunnelStore.subscribe(setTunnelState), []);
  useEffect(() => themeStore.subscribe(setTheme), []);

  const mtuValid = (() => {
    const n = Number(mtuRaw);
    return Number.isInteger(n) && n >= 576 && n <= 1500;
  })();

  const dnsUpstreamValid = (() => {
    if (!dnsUpstreamRaw.trim()) return false;
    return dnsUpstreamRaw
      .split(',').map(s => s.trim()).filter(Boolean)
      .every(ip => /^\d{1,3}(\.\d{1,3}){3}$/.test(ip) && ip.split('.').every(p => +p >= 0 && +p <= 255));
  })();

  const locked = tunnelState === 'connected' || tunnelState === 'connecting';

  const update = <K extends keyof AppSettings>(key: K, value: AppSettings[K]) => {
    setSettings(s => {
      const next = { ...s, [key]: value };
      settingsStore.save(next);
      return next;
    });
  };

  useEffect(() => {
    GetAutoStart().then(v => {
      if (v !== settings.autoStart) update('autoStart', v);
    }).catch(() => {});
  }, []);

  const commitMtu = () => {
    const n = Number(mtuRaw);
    const clamped = Number.isFinite(n) ? Math.max(576, Math.min(1500, Math.round(n))) : 1280;
    setMtuRaw(String(clamped));
    update('mtu', clamped);
  };

  const commitDnsUpstream = () => {
    if (dnsUpstreamValid) {
      update('dnsUpstream', dnsUpstreamRaw);
    } else {
      setDnsUpstreamRaw(settings.dnsUpstream);
    }
  };

  const commitWgIface = () => {
    update('wgInterface', wgIface.trim() || 'WDTT');
  };

  return (
    <>
      <style>{`
        .sp-wrap { display: flex; flex-direction: column; gap: 14px; animation: page-in 0.3s ease-out; }
        .sp-header { display: flex; align-items: center; gap: 10px; padding: 4px 4px 0; margin: 0 4px; }
        .sp-title { font-size: 22px; font-weight: 700; color: var(--text); flex: 1; }
        .sp-hint { font-size: 11px; color: var(--text-3); padding: 0 4px; }
        .sp-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--r-card); padding: 6px 22px; box-shadow: var(--shadow); margin: 0 16px; }
        .sp-section-label { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.5px; font-weight: 600; padding: 4px 4px 0; margin: 0 4px; }
        .sp-row { display: flex; align-items: center; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid var(--border-2); gap: 12px; }
        .sp-row:last-child { border-bottom: none; }
        .sp-row-locked { opacity: 0.45; pointer-events: none; }
        .sp-label { color: var(--text); font-size: 14px; display: flex; flex-direction: column; gap: 2px; }
        .sp-label-main { display: flex; align-items: center; gap: 10px; }
        .sp-label-main svg { color: var(--text-3); flex-shrink: 0; }
        .sp-label-sub { font-size: 11px; color: var(--text-3); padding-left: 26px; }
        .sp-toggle { width: 48px; height: 26px; border-radius: var(--r-toggle); border: 1.5px solid var(--input-border); background: var(--bg-2); cursor: pointer; position: relative; transition: background 0.2s, border-color 0.2s; flex-shrink: 0; padding: 0; }
        .sp-toggle::after { content: ''; position: absolute; width: 18px; height: 18px; border-radius: 50%; background: var(--text-3); top: 2px; left: 3px; transition: left 0.2s, background 0.2s; }
        .sp-toggle--on { background: var(--accent); border-color: var(--accent); }
        .sp-toggle--on::after { background: var(--accent-fg); left: 25px; }
        .sp-seg { display: flex; background: var(--seg-bg); border-radius: var(--r-pill); padding: 3px; gap: 2px; }
        .sp-seg-btn { padding: 7px 14px; border: none; border-radius: var(--r-pill); font-size: 12px; font-weight: 600; cursor: pointer; background: transparent; color: var(--seg-text); transition: background 0.15s, color 0.15s; font-family: 'Geist', sans-serif; }
        .sp-seg-btn--active { background: var(--accent); color: var(--accent-fg); }
        .sp-input { padding: 9px 12px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 14px; font-family: 'Geist Mono', monospace; outline: none; width: 100%; box-sizing: border-box; }
        .sp-input:focus { border-color: var(--accent); }
        .sp-input--error { border-color: var(--danger); }
        .sp-input--narrow { width: 140px; text-align: left; }
        .sp-theme-pill { display: flex; background: var(--seg-bg); border-radius: var(--r-pill); padding: 3px; gap: 2px; }
      `}</style>

      <div className="sp-wrap">
        <div className="sp-header">
          <IconSettings2 size={22} stroke={2} />
          <div className="sp-title">Настройки</div>
        </div>

        {locked && <div className="sp-hint">Туннель активен — DNS/WG настройки заблокированы. Отключите туннель для изменений.</div>}

        {/* Tunnel */}
        <div className="sp-section-label">Туннель</div>
        <div className="sp-card">
          <div className="sp-row">
            <span className="sp-label">
              <span className="sp-label-main">
                <IconRotate size={16} />
                Режим обхода
              </span>
            </span>
            <div className="sp-seg">
              <button
                className={`sp-seg-btn${settings.bypassMode === 'АВТ' ? ' sp-seg-btn--active' : ''}`}
                onClick={() => update('bypassMode', 'АВТ')}
              >АВТ</button>
              <button
                className={`sp-seg-btn${settings.bypassMode === 'РУЧ' ? ' sp-seg-btn--active' : ''}`}
                onClick={() => update('bypassMode', 'РУЧ')}
              >РУЧ</button>
            </div>
          </div>

          <div className={`sp-row${locked ? ' sp-row-locked' : ''}`}>
            <span className="sp-label">
              <span className="sp-label-main">MTU</span>
              <span className="sp-label-sub">576–1500</span>
            </span>
            <input
              type="number" min={576} max={1500} step={1}
              value={mtuRaw}
              className={`sp-input sp-input--narrow${!mtuValid ? ' sp-input--error' : ''}`}
              onChange={e => setMtuRaw(e.target.value)}
              onBlur={commitMtu}
            />
          </div>
        </div>

        {/* DNS & WireGuard */}
        <div className="sp-section-label">DNS и WireGuard</div>
        <div className="sp-card">
          <div className={`sp-row${locked ? ' sp-row-locked' : ''}`}>
            <span className="sp-label">
              <span className="sp-label-main">
                <IconWorld size={16} />
                Локальный DNS-прокси
              </span>
              <span className="sp-label-sub">127.0.0.1:53, защита от перехвата ISP</span>
            </span>
            <button
              className={`sp-toggle${settings.dnsProxyEnabled ? ' sp-toggle--on' : ''}`}
              onClick={() => update('dnsProxyEnabled', !settings.dnsProxyEnabled)}
            />
          </div>

          <div className={`sp-row${locked ? ' sp-row-locked' : ''}`} style={{ alignItems: 'flex-start' }}>
            <span className="sp-label" style={{ paddingTop: 6 }}>
              <span className="sp-label-main">Upstream DNS</span>
              <span className="sp-label-sub">через запятую, IP-адреса</span>
            </span>
            <input
              className={`sp-input${!dnsUpstreamValid ? ' sp-input--error' : ''}`}
              style={{ width: 200, textAlign: 'left' }}
              value={dnsUpstreamRaw}
              onChange={e => setDnsUpstreamRaw(e.target.value)}
              onBlur={commitDnsUpstream}
              placeholder="8.8.8.8,1.1.1.1"
              disabled={!settings.dnsProxyEnabled}
            />
          </div>

          <div className={`sp-row${locked ? ' sp-row-locked' : ''}`}>
            <span className="sp-label">
              <span className="sp-label-main">
                <IconShield size={16} />
                Авто-WireGuard
              </span>
              <span className="sp-label-sub">поднимать Windows WG интерфейс</span>
            </span>
            <button
              className={`sp-toggle${settings.autoWG ? ' sp-toggle--on' : ''}`}
              onClick={() => update('autoWG', !settings.autoWG)}
            />
          </div>

          <div className={`sp-row${locked ? ' sp-row-locked' : ''}`}>
            <span className="sp-label">
              <span className="sp-label-main">Имя WG-интерфейса</span>
            </span>
            <input
              className="sp-input sp-input--narrow"
              value={wgIface}
              onChange={e => setWgIface(e.target.value)}
              onBlur={commitWgIface}
            />
          </div>
        </div>

        {/* Link & Behavior */}
        <div className="sp-section-label">Поведение</div>
        <div className="sp-card">
          <div className="sp-row">
            <span className="sp-label">
              <span className="sp-label-main">
                <IconWorld size={16} />
                Режим ссылки
              </span>
              <span className="sp-label-sub">автоматически обрабатывать wdtt:// ссылки из буфера</span>
            </span>
            <button
              className={`sp-toggle${settings.linkMode ? ' sp-toggle--on' : ''}`}
              onClick={() => update('linkMode', !settings.linkMode)}
            />
          </div>
        </div>

        {/* System */}
        <div className="sp-section-label">Система</div>
        <div className="sp-card">
          <div className="sp-row">
            <span className="sp-label">
              <span className="sp-label-main">Тема</span>
            </span>
            <div className="sp-theme-pill">
              <button
                className={`sp-seg-btn${theme === 'light' ? ' sp-seg-btn--active' : ''}`}
                onClick={() => { themeStore.set('light'); setTheme('light'); }}
              >
                <IconSun size={14} stroke={2} style={{ verticalAlign: 'middle', marginRight: 4 }} />
                Свет
              </button>
              <button
                className={`sp-seg-btn${theme === 'dark' ? ' sp-seg-btn--active' : ''}`}
                onClick={() => { themeStore.set('dark'); setTheme('dark'); }}
              >
                <IconMoon size={14} stroke={2} style={{ verticalAlign: 'middle', marginRight: 4 }} />
                Тьма
              </button>
            </div>
          </div>

          <div className="sp-row">
            <span className="sp-label">
              <span className="sp-label-main">
                <IconServer2 size={16} />
                Сворачивать в трей
              </span>
            </span>
            <button
              className={`sp-toggle${settings.tray ? ' sp-toggle--on' : ''}`}
              onClick={() => {
                const next = !settings.tray;
                update('tray', next);
                SetTrayEnabled(next);
                toastStore.show(next ? 'Трей включён' : 'Трей выключен', 2000);
              }}
            />
          </div>

          <div className="sp-row">
            <span className="sp-label">
              <span className="sp-label-main">Запускать с Windows</span>
            </span>
            <button
              className={`sp-toggle${settings.autoStart ? ' sp-toggle--on' : ''}`}
              onClick={() => {
                const next = !settings.autoStart;
                update('autoStart', next);
                SetAutoStart(next);
                toastStore.show(next ? 'Автозапуск включён' : 'Автозапуск выключен', 2000);
              }}
            />
          </div>
        </div>
      </div>
    </>
  );
}
