import { useState, useEffect, useRef } from 'react';
import { IconServer2, IconServerOff, IconTerminal2, IconDice5 } from '@tabler/icons-react';
import { deployStore } from '../lib/store';
import type { DeployConfig, DeployState } from '../lib/types';
import { Deploy as WailsDeploy, Undeploy as WailsUndeploy } from '../../wailsjs/go/backend/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

export default function Deploy() {
  const [cfg, setCfg] = useState<DeployConfig>(() => deployStore.get());
  const [deployState, setDeployState] = useState<DeployState>('idle');
  const [logs, setLogs] = useState<string[]>([]);
  const logRef = useRef<HTMLDivElement>(null);

  const set = <K extends keyof DeployConfig>(k: K, v: DeployConfig[K]) => {
    const next = { ...cfg, [k]: v };
    setCfg(next);
    deployStore.save(next);
  };

  useEffect(() => {
    const offLog = EventsOn('deploy_log', (msg: string) => {
      setLogs(prev => [...prev, msg]);
    });
    const offDone = EventsOn('deploy_done', () => setDeployState('idle'));
    return () => { offLog(); offDone(); };
  }, []);

  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight);
  }, [logs]);

  const buildParams = () => ({
    host: cfg.host.trim(),
    login: cfg.login.trim() || 'root',
    password: cfg.password,
    sshPort: cfg.sshPort || '22',
    mainPassword: cfg.tunnelPassword,
    adminId: cfg.tgAdminId,
    botToken: cfg.tgBotToken,
    dtlsPort: cfg.portsManual ? parseInt(cfg.dtlsPort) || 56000 : 56000,
    wgPort: cfg.portsManual ? parseInt(cfg.wgPort) || 56001 : 56001,
  });

  const canDeploy = cfg.host.trim() && cfg.password.trim() && cfg.tunnelPassword.trim();
  const busy = deployState !== 'idle';

  const generatePassword = () => {
    const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789';
    const pwd = Array.from({ length: 16 }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
    set('tunnelPassword', pwd);
  };

  const handleInstall = async () => {
    if (!canDeploy || busy) return;
    setLogs([]);
    setDeployState('deploying');
    try {
      await WailsDeploy(buildParams());
    } catch (e: any) {
      setLogs(prev => [...prev, '❌ ' + String(e)]);
      setDeployState('idle');
    }
  };

  const handleRemove = async () => {
    if (!cfg.host.trim() || !cfg.password.trim() || busy) return;
    setLogs([]);
    setDeployState('removing');
    try {
      await WailsUndeploy(buildParams());
    } catch (e: any) {
      setLogs(prev => [...prev, '❌ ' + String(e)]);
      setDeployState('idle');
    }
  };

  return (
    <>
      <style>{`
        .dp-wrap { display: flex; flex-direction: column; gap: 14px; animation: page-in 0.3s ease-out; }
        .dp-header { display: flex; align-items: center; gap: 10px; padding: 4px 4px 0; }
        .dp-title { font-size: 22px; font-weight: 700; color: var(--text); flex: 1; }
        .dp-status-pill { display: inline-flex; align-items: center; gap: 6px; padding: 5px 12px; border-radius: var(--r-pill); font-size: 12px; font-weight: 600; }
        .dp-status-pill--idle { background: var(--bg-2); color: var(--text-2); }
        .dp-status-pill--deploying { background: var(--accent-soft); color: var(--accent); }
        .dp-status-pill--removing { background: rgba(214, 69, 69, 0.12); color: var(--danger); }
        .dp-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--r-card); padding: 6px 22px; box-shadow: var(--shadow); margin: 0 16px; }
        .dp-card:last-of-type { margin-bottom: 0; }
        .dp-section-label { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.5px; font-weight: 600; padding: 4px 4px 0; }
        .dp-row { display: flex; align-items: center; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid var(--border-2); gap: 12px; }
        .dp-row:last-child { border-bottom: none; }
        .dp-label { color: var(--text); font-size: 14px; min-width: 110px; }
        .dp-input { padding: 9px 12px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 14px; font-family: 'Geist Mono', monospace; outline: none; flex: 1; min-width: 0; max-width: 260px; }
        .dp-input:focus { border-color: var(--accent); }
        .dp-input::placeholder { color: var(--text-4); }
        .dp-input--narrow { max-width: 110px; }
        .dp-input-with-btn { display: flex; align-items: center; gap: 6px; flex: 1; max-width: 260px; }
        .dp-icon-btn { width: 34px; height: 34px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); cursor: pointer; display: flex; align-items: center; justify-content: center; color: var(--text-3); transition: background 0.12s, color 0.12s; padding: 0; flex-shrink: 0; }
        .dp-icon-btn:hover { background: var(--bg-2); color: var(--accent); }
        .dp-toggle { width: 48px; height: 26px; border-radius: var(--r-toggle); border: 1.5px solid var(--input-border); background: var(--bg-2); cursor: pointer; position: relative; transition: background 0.2s, border-color 0.2s; flex-shrink: 0; padding: 0; }
        .dp-toggle::after { content: ''; position: absolute; width: 18px; height: 18px; border-radius: 50%; background: var(--text-3); top: 2px; left: 3px; transition: left 0.2s, background 0.2s; }
        .dp-toggle--on { background: var(--accent); border-color: var(--accent); }
        .dp-toggle--on::after { background: var(--accent-fg); left: 25px; }
        .dp-actions { display: flex; gap: 10px; margin: 4px 16px 0; }
        .dp-btn { flex: 1; padding: 14px; border: none; border-radius: var(--r-btn); font-family: 'Geist', sans-serif; font-size: 14px; font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 8px; transition: opacity 0.2s; }
        .dp-btn:disabled { opacity: 0.45; cursor: not-allowed; }
        .dp-btn--accent { background: var(--accent); color: var(--accent-fg); }
        .dp-btn--danger { background: var(--danger); color: #fff; }
        .dp-log-card { background: #0d0d12; color: #B5B5CC; border-radius: var(--r-card); padding: 16px 18px; font-size: 12px; font-family: 'Geist Mono', monospace; max-height: 280px; overflow-y: auto; white-space: pre-wrap; word-break: break-all; box-shadow: var(--shadow); margin: 0 16px; }
        .dp-log-empty { color: #5C5C7A; text-align: center; padding: 20px 0; font-size: 13px; }
        .dp-log-header { display: flex; align-items: center; gap: 8px; color: #B5B5CC; font-family: 'Geist', sans-serif; font-weight: 600; font-size: 13px; margin-bottom: 8px; }
      `}</style>

      <div className="dp-wrap">
        <div className="dp-header">
          <IconServer2 size={22} stroke={2} />
          <div className="dp-title">Деплой VPS</div>
          <div className={`dp-status-pill dp-status-pill--${deployState}`}>
            {deployState === 'idle' ? 'Готов' : deployState === 'deploying' ? 'Установка…' : 'Удаление…'}
          </div>
        </div>

        {/* SSH card */}
        <div className="dp-section-label">SSH-доступ</div>
        <div className="dp-card">
          <div className="dp-row">
            <span className="dp-label">Хост</span>
            <input
              className="dp-input"
              placeholder="IP или домен (без порта)"
              value={cfg.host}
              onChange={e => set('host', e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="dp-row">
            <span className="dp-label">Логин</span>
            <input
              className="dp-input"
              placeholder="root"
              value={cfg.login}
              onChange={e => set('login', e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="dp-row">
            <span className="dp-label">Пароль SSH</span>
            <input
              className="dp-input"
              placeholder="пароль"
              value={cfg.password}
              onChange={e => set('password', e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="dp-row">
            <span className="dp-label">Порт SSH</span>
            <input
              className="dp-input dp-input--narrow"
              placeholder="22"
              value={cfg.sshPort}
              onChange={e => set('sshPort', e.target.value)}
              disabled={busy}
            />
          </div>
        </div>

        {/* Tunnel + bot card */}
        <div className="dp-section-label">Туннель и бот</div>
        <div className="dp-card">
          <div className="dp-row">
            <span className="dp-label">Пароль туннеля</span>
            <div className="dp-input-with-btn">
              <input
                className="dp-input"
                placeholder="16-символов"
                value={cfg.tunnelPassword}
                onChange={e => set('tunnelPassword', e.target.value)}
                disabled={busy}
              />
              <button
                className="dp-icon-btn"
                onClick={generatePassword}
                disabled={busy}
                title="Сгенерировать"
              >
                <IconDice5 size={16} />
              </button>
            </div>
          </div>
          <div className="dp-row">
            <span className="dp-label">ID админа TG</span>
            <input
              className="dp-input"
              placeholder="опционально"
              value={cfg.tgAdminId}
              onChange={e => set('tgAdminId', e.target.value)}
              disabled={busy}
            />
          </div>
          <div className="dp-row">
            <span className="dp-label">Токен бота TG</span>
            <input
              className="dp-input"
              placeholder="опционально"
              value={cfg.tgBotToken}
              onChange={e => set('tgBotToken', e.target.value)}
              disabled={busy}
            />
          </div>
        </div>

        {/* Ports card */}
        <div className="dp-section-label">Порты</div>
        <div className="dp-card">
          <div className="dp-row">
            <span className="dp-label">Ручные порты</span>
            <button
              className={`dp-toggle${cfg.portsManual ? ' dp-toggle--on' : ''}`}
              onClick={() => set('portsManual', !cfg.portsManual)}
              disabled={busy}
            />
          </div>
          {cfg.portsManual && (
            <>
              <div className="dp-row">
                <span className="dp-label">DTLS</span>
                <input
                  className="dp-input dp-input--narrow"
                  placeholder="56000"
                  value={cfg.dtlsPort}
                  onChange={e => set('dtlsPort', e.target.value)}
                  disabled={busy}
                />
              </div>
              <div className="dp-row">
                <span className="dp-label">WireGuard</span>
                <input
                  className="dp-input dp-input--narrow"
                  placeholder="56001"
                  value={cfg.wgPort}
                  onChange={e => set('wgPort', e.target.value)}
                  disabled={busy}
                />
              </div>
            </>
          )}
        </div>

        {/* Actions */}
        <div className="dp-actions">
          <button className="dp-btn dp-btn--accent" onClick={handleInstall} disabled={!canDeploy || busy}>
            <IconServer2 size={18} />
            {deployState === 'deploying' ? 'Установка…' : 'Установить'}
          </button>
          <button className="dp-btn dp-btn--danger" onClick={handleRemove} disabled={!cfg.host.trim() || !cfg.password.trim() || busy}>
            <IconServerOff size={18} />
            {deployState === 'removing' ? 'Удаление…' : 'Удалить'}
          </button>
        </div>

        {/* Log */}
        {(logs.length > 0 || busy) && (
          <div className="dp-log-card" ref={logRef}>
            <div className="dp-log-header">
              <IconTerminal2 size={14} />
              Лог деплоя
            </div>
            {logs.length === 0 ? <div className="dp-log-empty">ожидание вывода…</div> : logs.join('\n')}
          </div>
        )}
      </div>
    </>
  );
}
