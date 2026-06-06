import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  IconHash,
  IconPlayerStopFilled,
  IconKey,
  IconBolt,
  IconLink,
  IconChevronRight,
} from '@tabler/icons-react';
import AddServer from '../modals/Add-server';
import EditServer from '../modals/Edit-server';
import PasteLink from '../modals/PasteLink';
import HashEditor from '../modals/Hash';
import Secrets from '../modals/Secrets';
import { serverStore, settingsStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { toastStore } from '../lib/stores/toastStore';
import { wdttLinkStore } from '../lib/utils/wdttLink';
import { SaveProfile, Connect as WailsConnect, Disconnect as WailsDisconnect } from '../../wailsjs/go/backend/App';
import type { Server, TunnelState, AppSettings } from '../lib/types';

const TUNNEL_LABEL: Record<TunnelState, string> = {
  idle: 'Отключено',
  connecting: 'Подключение…',
  connected: 'Подключено',
  disconnecting: 'Отключение…',
};

export default function Tunnel() {
  const navigate = useNavigate();
  const [servers, setServers] = useState<Server[]>(() => serverStore.getAll());
  const [selected, setSelected] = useState<Server | null>(() => {
    const all = serverStore.getAll();
    return all.length > 0 ? all[0] : null;
  });
  const [tunnelState, setTunnelState] = useState<TunnelState>(() => tunnelStore.get());
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  const [settings, setSettings] = useState(() => settingsStore.get());
  const [hashOpen, setHashOpen] = useState(false);
  const [secretsOpen, setSecretsOpen] = useState(false);
  const [pasteLinkOpen, setPasteLinkOpen] = useState(false);
  const [addServerOpen, setAddServerOpen] = useState(false);
  const [editServer, setEditServer] = useState<Server | null>(null);
  const [reconnectAt, setReconnectAt] = useState(0);

  // wdtt:// paste handler (only if linkMode is on)
  useEffect(() => {
    const cur = settingsStore.get();
    if (!cur.linkMode) return;
    return wdttLinkStore.subscribe((link) => {
      if (!link) return;
      const consumed = wdttLinkStore.consume();
      if (!consumed) return;
      const host = `${consumed.ip}:${consumed.dtlsPort}`;
      const name = consumed.name;
      const finish = async (saveHashes: boolean) => {
        await SaveProfile(name, {
          peer: host, password: consumed.password, hashes: saveHashes ? consumed.hashes : [],
          turn: '', port: '', device_id: '', listen: '',
        });
        const existing = serverStore.getAll().find(s => s.host === host);
        const s = existing ?? serverStore.add({
          name, host, password: consumed.password,
          hashes: saveHashes ? (consumed.hashes.slice(0, 4) as [string, string, string, string]) : ['', '', '', ''],
          useGlobalHashes: !saveHashes,
          power: 9,
        });
        setServers(serverStore.getAll());
        setSelected(s);
        toastStore.show(existing ? `Сервер обновлён: ${name}` : `Сервер добавлен: ${name}`, 3000);
      };
      if (consumed.hashes.length > 0) {
        const yes = window.confirm('Ссылка содержит хеши. Перезаписать текущие хеши?');
        finish(yes);
      } else {
        finish(false);
      }
    });
  }, []);

  const doConnect = async () => {
    if (!selected) return;
    const s = settingsStore.get();
    const useGlobal = selected.useGlobalHashes;
    const filled = (useGlobal ? s.hashes : selected.hashes).filter(h => h.trim());
    if (filled.length === 0) {
      toastStore.show(useGlobal
        ? 'Добавьте глобальные хеши или заполните хеши профиля'
        : 'Заполните хеши VK в настройках сервера', 3500);
      return;
    }
    tunnelStore.set('connecting');
    const dnsUpstream = s.dnsUpstream.split(',').map(p => p.trim()).filter(Boolean);
    try {
      await WailsConnect({
        profile: selected.name,
        captchaMode: 'auto',
        workers: selected.power || 9,
        mtu: s.mtu || 1280,
        hashes: filled,
        autoWG: s.autoWG,
        noDNSProxy: !s.dnsProxyEnabled,
        dnsUpstream: dnsUpstream.length > 0 ? dnsUpstream : undefined,
        wgInterface: s.wgInterface || 'WDTT',
      });
      navigate('/logs');
    } catch {
      tunnelStore.set('idle');
    }
  };

  const handleConnect = async () => {
    if (!selected) {
      setAddServerOpen(true);
      return;
    }
    if (tunnelState === 'idle') {
      if (Date.now() < reconnectAt) {
        const secs = Math.ceil((reconnectAt - Date.now()) / 1000);
        toastStore.show(`Подождите ${secs} сек.`, 2000);
        return;
      }
      toastStore.show('Запускаю туннель', 2000);
      await doConnect();
    } else if (tunnelState === 'connected' || tunnelState === 'connecting') {
      tunnelStore.set('disconnecting');
      await WailsDisconnect();
      tunnelStore.set('idle');
      setReconnectAt(Date.now() + 4000);
    }
  };

  const handleAdd = (data: Omit<Server, 'id'>) => {
    const s = serverStore.add(data);
    setServers(serverStore.getAll());
    setSelected(s);
  };

  const handleApplyLink = async (link: { ip: string; dtlsPort: string; password: string; hashes: string[]; name: string }) => {
    const host = `${link.ip}:${link.dtlsPort}`;
    const name = link.name;
    await SaveProfile(name, {
      peer: host, password: link.password, hashes: link.hashes,
      turn: '', port: '', device_id: '', listen: '',
    });
    const existing = serverStore.getAll().find(s => s.host === host);
    const s = existing ?? serverStore.add({
      name, host, password: link.password,
      hashes: (link.hashes.slice(0, 4) as [string, string, string, string]).length > 0
        ? (link.hashes.slice(0, 4) as [string, string, string, string])
        : ['', '', '', ''],
      useGlobalHashes: link.hashes.length === 0,
      power: 9,
    });
    setServers(serverStore.getAll());
    setSelected(s);
    if (link.hashes.length > 0) {
      toastStore.show(`Профиль создан + ${link.hashes.length} хешей`, 3000);
    } else {
      toastStore.show(`Профиль ${existing ? 'обновлён' : 'создан'}: ${name}`, 3000);
    }
  };

  const handleSave = (server: Server) => {
    serverStore.update(server);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === server.id) setSelected(server);
  };

  const handleDelete = (id: string) => {
    serverStore.remove(id);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === id) setSelected(all[0] ?? null);
  };

  const filledHashes = selected
    ? (selected.useGlobalHashes
        ? settings.hashes.filter(h => h.trim()).length
        : selected.hashes.filter(h => h.trim()).length)
    : 0;

  return (
    <>
      <style>{`
        .tn-page { display: flex; flex-direction: column; gap: 14px; animation: page-in 0.3s ease-out; }
        .tn-title { font-size: 22px; font-weight: 700; color: var(--text); margin: 0 4px 6px; padding: 0; }
        .tn-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--r-card); padding: 18px 18px; box-shadow: var(--shadow); margin: 0 16px; }
        .tn-card:last-of-type { margin-bottom: 0; }
        .tn-field { display: flex; flex-direction: column; gap: 6px; }
        .tn-field-label { font-size: 12px; color: var(--text-3); font-weight: 500; padding-left: 2px; }
        .tn-server-trigger { width: 100%; padding: 12px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 14px; font-family: 'Geist Mono', monospace; font-weight: 500; cursor: pointer; display: flex; align-items: center; gap: 10px; transition: border-color 0.15s; text-align: left; }
        .tn-server-trigger:hover { border-color: var(--text-3); }
        .tn-server-trigger:focus { border-color: var(--accent); outline: none; }
        .tn-server-trigger-empty { color: var(--text-4); font-family: 'Geist', sans-serif; }
        .tn-server-trigger-name { flex: 1; }
        .tn-server-trigger-host { color: var(--text-3); font-size: 12px; }
        .tn-server-trigger-power { color: var(--accent); font-size: 11px; font-weight: 600; padding: 2px 6px; border-radius: 6px; background: var(--accent-soft); }
        .tn-hash-btn { width: 100%; margin-top: 10px; padding: 12px 14px; border: 1.5px solid var(--border); border-radius: var(--r-input); background: var(--surface-2); color: var(--text); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 8px; transition: background 0.12s, border-color 0.12s; }
        .tn-hash-btn:hover { background: var(--bg-2); border-color: var(--text-3); }
        .tn-hash-btn-count { margin-left: auto; color: var(--text-3); font-size: 12px; font-weight: 500; }
        .tn-hash-source { font-size: 11px; color: var(--text-3); margin-top: 4px; padding-left: 2px; }
        .tn-toggle-row { display: flex; align-items: center; justify-content: space-between; padding: 12px 0; }
        .tn-toggle-row-label { color: var(--text); font-size: 14px; }
        .tn-divider { height: 1px; background: var(--border-2); margin: 0; }
        .tn-toggle { width: 48px; height: 26px; border-radius: var(--r-toggle); border: 1.5px solid var(--input-border); background: var(--bg-2); cursor: pointer; position: relative; transition: background 0.2s, border-color 0.2s; flex-shrink: 0; padding: 0; }
        .tn-toggle::after { content: ''; position: absolute; width: 18px; height: 18px; border-radius: 50%; background: var(--text-3); top: 2px; left: 3px; transition: left 0.2s, background 0.2s; }
        .tn-toggle--on { background: var(--accent); border-color: var(--accent); }
        .tn-toggle--on::after { background: var(--accent-fg); left: 25px; }
        .tn-link-row { display: flex; align-items: center; gap: 10px; width: 100%; padding: 12px 0; background: none; border: none; border-radius: 0; cursor: pointer; color: var(--text); font-family: 'Geist', sans-serif; font-size: 14px; text-align: left; }
        .tn-link-row:hover .tn-link-row-label { color: var(--accent); }
        .tn-link-row-label { flex: 1; transition: color 0.15s; }
        .tn-actions { display: flex; gap: 10px; margin: 0 16px; }
        .tn-action { flex: 1; padding: 14px 18px; border-radius: var(--r-btn); font-family: 'Geist', sans-serif; font-size: 15px; font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 8px; transition: background 0.12s, opacity 0.12s, border-color 0.12s; }
        .tn-action:disabled { opacity: 0.5; cursor: not-allowed; }
        .tn-action--outlined { background: var(--surface); color: var(--text); border: 1.5px solid var(--border); }
        .tn-action--outlined:hover:not(:disabled) { background: var(--surface-2); border-color: var(--text-3); }
        .tn-action--filled { background: var(--accent); color: var(--accent-fg); border: 1.5px solid var(--accent); }
        .tn-action--filled:hover:not(:disabled) { opacity: 0.92; }
        .tn-action--danger { background: var(--danger); color: #fff; border: 1.5px solid var(--danger); }
      `}</style>

      <div className="tn-page">
        <h1 className="tn-title">Настройки туннеля</h1>

        {/* CARD 1: IP + Hash btn */}
        <div className="tn-card">
          <div className="tn-field">
            <span className="tn-field-label">IP-адрес сервера</span>
            <button
              className={`tn-server-trigger${!selected ? ' tn-server-trigger-empty' : ''}`}
              onClick={() => {
                if (!selected && servers.length === 0) {
                  setAddServerOpen(true);
                } else if (selected) {
                  setEditServer(selected);
                }
              }}
            >
              {selected ? (
                <>
                  <IconLink size={16} style={{ color: 'var(--text-3)' }} />
                  <span className="tn-server-trigger-name">{selected.host}</span>
                  <span className="tn-server-trigger-power">{selected.power}w</span>
                  <span className="tn-server-trigger-host">{selected.name}</span>
                </>
              ) : (
                <>
                  <IconLink size={16} style={{ color: 'var(--text-3)' }} />
                  <span className="tn-server-trigger-name">Нажмите, чтобы добавить сервер</span>
                </>
              )}
            </button>
          </div>
          <button className="tn-hash-btn" onClick={() => setHashOpen(true)}>
            <IconHash size={16} stroke={2} />
            Настройка VK Хешей
            <span className="tn-hash-btn-count">{filledHashes}/4</span>
          </button>
          {selected && (
            <div className="tn-hash-source">
              {selected.useGlobalHashes ? 'источник: глобальные хеши' : 'источник: хеши профиля'}
            </div>
          )}
        </div>

        {/* CARD 2: Toggles + Link paste */}
        <div className="tn-card">
          <div className="tn-toggle-row">
            <span className="tn-toggle-row-label">Авто капча</span>
            <button
              className={`tn-toggle${settings.bypassMode === 'АВТ' ? ' tn-toggle--on' : ''}`}
              onClick={() => {
                const next: AppSettings = { ...settings, bypassMode: settings.bypassMode === 'АВТ' ? 'РУЧ' : 'АВТ' };
                setSettings(next);
                settingsStore.save(next);
              }}
            />
          </div>
          <div className="tn-divider" />
          <button
            className="tn-link-row"
            onClick={() => setPasteLinkOpen(true)}
            type="button"
          >
            <IconLink size={16} style={{ color: 'var(--text-3)' }} />
            <span className="tn-link-row-label">Вставить wdtt:// ссылку</span>
            <IconChevronRight size={16} style={{ color: 'var(--text-3)' }} />
          </button>
        </div>

        {/* ACTIONS: Secrets + Connect */}
        <div className="tn-actions">
          <button
            className="tn-action tn-action--outlined"
            onClick={() => {
              if (!selected) {
                setAddServerOpen(true);
                return;
              }
              setSecretsOpen(true);
            }}
          >
            <IconKey size={18} stroke={2} />
            Секреты
          </button>
          <button
            className={`tn-action ${tunnelState === 'connected' || tunnelState === 'connecting' ? 'tn-action--danger' : 'tn-action--filled'}`}
            onClick={handleConnect}
          >
            {tunnelState === 'connected' || tunnelState === 'connecting' ? (
              <>
                <IconPlayerStopFilled size={18} />
                {tunnelState === 'connected' ? 'Отключить' : 'Подключение…'}
              </>
            ) : (
              <>
                <IconBolt size={18} />
                {TUNNEL_LABEL[tunnelState] === 'Отключение…' ? 'Отключение…' : 'Подключить'}
              </>
            )}
          </button>
        </div>
      </div>

      {addServerOpen && <AddServer onClose={() => setAddServerOpen(false)} onAdd={handleAdd} />}
      {editServer && (
        <EditServer
          server={editServer}
          onClose={() => setEditServer(null)}
          onSave={handleSave}
          onDelete={handleDelete}
        />
      )}
      {hashOpen && selected && (
        <HashEditor
          hashes={selected.useGlobalHashes ? settings.hashes : selected.hashes}
          onClose={() => setHashOpen(false)}
          onSave={hashes => {
            if (selected.useGlobalHashes) {
              const next = { ...settings, hashes };
              setSettings(next);
              settingsStore.save(next);
            } else {
              const updated = { ...selected, hashes };
              handleSave(updated);
            }
          }}
        />
      )}
      {secretsOpen && selected && (
        <Secrets
          server={selected}
          onClose={() => setSecretsOpen(false)}
        />
      )}
      {pasteLinkOpen && (
        <PasteLink
          onClose={() => setPasteLinkOpen(false)}
          onApply={handleApplyLink}
        />
      )}
    </>
  );
}
