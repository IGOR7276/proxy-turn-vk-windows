import { useState, useEffect } from 'react';
import { IconSettings2, IconServer2, IconWorld, IconShield, IconSun, IconMoon, IconRotate, IconKey, IconAlertTriangle, IconX } from '@tabler/icons-react';
import { settingsStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { themeStore } from '../lib/stores/themeStore';
import { toastStore } from '../lib/stores/toastStore';
import type { AppSettings } from '../lib/types';
import { DNS_PRESETS } from '../lib/types';
import { SetTrayEnabled, SetAutoStart, GetAutoStart, SetCloseActionPreference } from '../../wailsjs/go/backend/App';

function extractHashInput(raw: string): string {
  const v = raw.trim();
  if (!v) return '';
  const lower = v.toLowerCase();
  const marker = '/call/join/';
  const idx = lower.indexOf(marker);
  if (idx !== -1) {
    return v.slice(idx + marker.length).split(/[?#\s]/)[0].trim();
  }
  return v.split(/[?#\s]/)[0].trim();
}

export default function Settings() {
  const [settings, setSettings] = useState<AppSettings>(() => settingsStore.get());
  const [theme, setTheme] = useState(() => themeStore.get());
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());
  const [mtuRaw, setMtuRaw] = useState(String(settings.mtu || 1380));
  const [dnsCustomRaw, setDnsCustomRaw] = useState(settings.dnsCustom || '');
  const [wgIface, setWgIface] = useState(settings.wgInterface || 'WDTT');

  useEffect(() => tunnelStore.subscribe(setTunnelState), []);
  useEffect(() => themeStore.subscribe(setTheme), []);

  const mtuValid = (() => {
    const n = Number(mtuRaw);
    return Number.isInteger(n) && n >= 576 && n <= 1500;
  })();

  const mtuPresets = [
    { v: 1280, label: '1280', hint: 'минимум' },
    { v: 1380, label: '1380', hint: 'игры' },
    { v: 1420, label: '1420', hint: 'макс' },
  ];

  const dnsCustomValid = (() => {
    if (!dnsCustomRaw.trim()) return false;
    return dnsCustomRaw
      .split(',').map(s => s.trim()).filter(Boolean)
      .every(ip => /^\d{1,3}(\.\d{1,3}){3}$/.test(ip) && ip.split('.').every(p => +p >= 0 && +p <= 255));
  })();

  const commitDnsCustom = () => {
    if (dnsCustomValid) {
      update('dnsCustom', dnsCustomRaw);
    } else {
      setDnsCustomRaw(settings.dnsCustom || '');
    }
  };

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
    const clamped = Number.isFinite(n) ? Math.max(576, Math.min(1500, Math.round(n))) : 1380;
    setMtuRaw(String(clamped));
    update('mtu', clamped);
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
        .sp-seg--compact .sp-seg-btn { padding: 5px 10px; font-size: 11px; white-space: nowrap; }
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
              <span className="sp-label-sub">576–1500 • 1280 без фрагментации, 1380 для игр, 1420 макс</span>
            </span>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, alignItems: 'flex-end' }}>
              <input
                type="number" min={576} max={1500} step={1}
                value={mtuRaw}
                className={`sp-input sp-input--narrow${!mtuValid ? ' sp-input--error' : ''}`}
                onChange={e => setMtuRaw(e.target.value)}
                onBlur={commitMtu}
              />
              <div style={{ display: 'flex', gap: 4 }}>
                {mtuPresets.map(p => (
                  <button
                    key={p.v}
                    type="button"
                    onClick={() => { setMtuRaw(String(p.v)); update('mtu', p.v); }}
                    title={p.hint}
                    style={{
                      padding: '3px 8px',
                      fontSize: 10,
                      fontWeight: 600,
                      border: '1px solid var(--input-border)',
                      borderRadius: 6,
                      background: Number(mtuRaw) === p.v ? 'var(--accent)' : 'var(--bg-2)',
                      color: Number(mtuRaw) === p.v ? 'var(--accent-fg)' : 'var(--text-3)',
                      cursor: 'pointer',
                      fontFamily: 'Geist Mono, monospace',
                    }}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            </div>
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
              <span className="sp-label-main">DNS-провайдер</span>
              <span className="sp-label-sub">
                {settings.dnsProvider === 'custom'
                  ? settings.dnsCustom
                  : DNS_PRESETS[settings.dnsProvider]?.servers}
              </span>
            </span>
            <div className="sp-seg" style={{ flexWrap: 'wrap' }}>
              {(['google', 'cloudflare', 'yandex', 'custom'] as const).map(p => (
                <button
                  key={p}
                  className={`sp-seg-btn${settings.dnsProvider === p ? ' sp-seg-btn--active' : ''}`}
                  onClick={() => update('dnsProvider', p)}
                  disabled={!settings.dnsProxyEnabled}
                >
                  {p === 'custom' ? 'Свой' : DNS_PRESETS[p].name}
                </button>
              ))}
            </div>
          </div>

          {settings.dnsProvider === 'custom' && (
            <div className={`sp-row${locked ? ' sp-row-locked' : ''}`} style={{ alignItems: 'flex-start' }}>
              <span className="sp-label" style={{ paddingTop: 6 }}>
                <span className="sp-label-main">Custom DNS</span>
                <span className="sp-label-sub">через запятую, IP-адреса</span>
              </span>
              <input
                className={`sp-input${!dnsCustomValid ? ' sp-input--error' : ''}`}
                style={{ width: 220, textAlign: 'left' }}
                value={dnsCustomRaw}
                onChange={e => setDnsCustomRaw(e.target.value)}
                onBlur={commitDnsCustom}
                placeholder="9.9.9.9,149.112.112.112"
                disabled={!settings.dnsProxyEnabled}
              />
            </div>
          )}

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

        {/* VK Hashes */}
        <div className="sp-section-label">VK Хеши (глобальные)</div>
        <div className="sp-card">
          <div className={`sp-row${locked ? ' sp-row-locked' : ''}`} style={{ alignItems: 'flex-start', flexDirection: 'column', gap: 10 }}>
            <span className="sp-label" style={{ width: '100%' }}>
              <span className="sp-label-main">
                <IconKey size={16} />
                Глобальные VK-ключи
              </span>
              <span className="sp-label-sub">используются всеми профилями с включённым «Глобальные хеши». Можно оставить пустыми, если не пользуетесь VK.</span>
            </span>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, width: '100%' }}>
              {settings.hashes.map((h, i) => (
                <input
                  key={i}
                  className="sp-input"
                  style={{ fontFamily: 'Geist Mono, monospace' }}
                  value={h}
                  onChange={e => {
                    const next: [string, string, string, string] = [...settings.hashes] as [string, string, string, string];
                    next[i] = extractHashInput(e.target.value);
                    update('hashes', next);
                  }}
                  placeholder={`ключ ${i + 1}`}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="off"
                />
              ))}
            </div>
            <div style={{ display: 'flex', gap: 8, width: '100%', alignItems: 'center' }}>
              {(() => {
                const filled = settings.hashes.filter(h => h.trim()).length;
                const dups = filled - new Set(settings.hashes.filter(h => h.trim())).size;
                if (dups > 0) {
                  return (
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 12, color: 'var(--danger)' }}>
                      <IconAlertTriangle size={14} stroke={2} />
                      дубликаты: {dups}
                    </span>
                  );
                }
                return <span style={{ fontSize: 12, color: 'var(--text-3)' }}>Заполнено: <strong style={{ color: 'var(--accent)' }}>{filled}</strong>/4</span>;
              })()}
              <div style={{ flex: 1 }} />
              <button
                className="sp-seg-btn"
                onClick={() => update('hashes', ['', '', '', ''])}
              >
                Очистить
              </button>
            </div>
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

          <div className="sp-row">
            <span className="sp-label">
              <span className="sp-label-main">
                <IconX size={16} />
                При нажатии на крестик
              </span>
              <span className="sp-label-sub">спрашивать / скрыть в трей / закрыть</span>
            </span>
            <div className="sp-seg sp-seg--compact">
              {([
                { v: 'ask', l: 'Спрашивать' },
                { v: 'hide', l: 'Скрыть' },
                { v: 'exit', l: 'Закрыть' },
              ] as const).map(o => (
                <button
                  key={o.v}
                  className={`sp-seg-btn${settings.closeAction === o.v ? ' sp-seg-btn--active' : ''}`}
                  onClick={() => {
                    update('closeAction', o.v);
                    SetCloseActionPreference(o.v);
                  }}
                >
                  {o.l}
                </button>
              ))}
            </div>
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
