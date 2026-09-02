import { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  IconHash,
  IconPlayerStopFilled,
  IconKey,
  IconBolt,
  IconLink,
  IconChevronRight,
  IconPlus,
  IconPencil,
  IconCheck,
  IconFileImport,
  IconWorld,
  IconRotate,
  IconFolder,
  IconTrash,
} from '@tabler/icons-react';
import AddServer from '../modals/Add-server';
import EditServer from '../modals/Edit-server';
import PasteLink from '../modals/PasteLink';
import ImportQwdtt from '../modals/ImportQwdtt';
import AddSubscriptionModal from '../modals/AddSubscription';
import HashEditor from '../modals/Hash';
import Secrets from '../modals/Secrets';
import { serverStore, settingsStore, selectionStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { toastStore } from '../lib/stores/toastStore';
import { wdttLinkStore } from '../lib/utils/wdttLink';
import { stripVkUrl } from '../lib/utils/qwdttParser';
import { SaveProfile, Connect as WailsConnect, Disconnect as WailsDisconnect, ForceDisconnect, GetVkAuthMode, VkLogin as BackendVkLogin, VkCallJoin, GetExcludeDomains, ListSubscriptions, UpdateSubscription, DeleteSubscription, OpenSubscriptionFolder, GetSubscriptionProfiles } from '../../wailsjs/go/backend/App';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';
import type { Server, TunnelState, AppSettings, Subscription } from '../lib/types';
import type { backend } from '../../wailsjs/go/models';
import { resolveDnsUpstream } from '../lib/types';

const TUNNEL_LABEL: Record<TunnelState, string> = {
  idle: 'Отключено',
  connecting: 'Подключение…',
  reconnecting: 'Переподключение…',
  connected: 'Подключено',
  disconnecting: 'Отключение…',
};

export default function Tunnel() {
  const navigate = useNavigate();
  const [servers, setServers] = useState<Server[]>(() => serverStore.getAll());
  const [selectedId, setSelectedId] = useState<string | null>(() => {
    const all = serverStore.getAll();
    const saved = selectionStore.get();
    if (saved && all.some(s => s.id === saved)) return saved;
    return all[0]?.id ?? null;
  });

  // Merge subscription profiles into local server store on mount.
  useEffect(() => {
    (async () => {
      try {
        const subs = await ListSubscriptions();
        if (!Array.isArray(subs) || subs.length === 0) return;
        const profs = await GetSubscriptionProfiles();
        for (const sub of subs) {
          syncSubscriptionProfiles(sub.id, profs);
        }
        const all = serverStore.getAll();
        setServers(all);
        const saved = selectionStore.get();
        if (saved && all.some(s => s.id === saved)) {
          setSelectedId(saved);
        } else if (!selectedId && all.length > 0) {
          setSelectedId(all[0].id);
          selectionStore.set(all[0].id);
        }
        const subCount = all.filter(s => s.subscriptionId).length;
        if (subCount > 0) {
          toastStore.show(`Загружено профилей из подписок: ${subCount}`, 2500);
        }
      } catch {
        // ignore
      }
    })();
  }, []);
  const selected = selectedId ? servers.find(s => s.id === selectedId) ?? null : null;
  const manualServers = servers.filter(s => !s.subscriptionId);
  const setSelected = useCallback((s: Server | null) => {
    const id = s?.id ?? null;
    setSelectedId(id);
    selectionStore.set(id);
  }, []);
  const [tunnelState, setTunnelState] = useState<TunnelState>(() => tunnelStore.get());
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  const selectedRef = useRef(selected);
  selectedRef.current = selected;

  const tunnelStateRef = useRef(tunnelState);
  tunnelStateRef.current = tunnelState;

  const [settings, setSettings] = useState(() => settingsStore.get());
  const [hashOpen, setHashOpen] = useState(false);
  const [secretsOpen, setSecretsOpen] = useState(false);
  const [pasteLinkOpen, setPasteLinkOpen] = useState(false);
  const [importQwdttOpen, setImportQwdttOpen] = useState(false);
  const [addServerOpen, setAddServerOpen] = useState(false);
  const [addSubOpen, setAddSubOpen] = useState(false);
  const [subscriptions, setSubscriptions] = useState<Subscription[]>([]);
  const [editServer, setEditServer] = useState<Server | null>(null);
  const [reconnectAt, setReconnectAt] = useState(0);
  const [vkAuthMode, setVkAuthMode] = useState<'anonymous' | 'account'>('anonymous');
  const loadSubscriptions = async () => {
    try {
      const subs = await ListSubscriptions();
      setSubscriptions(Array.isArray(subs) ? subs : []);
    } catch {
      setSubscriptions([]);
    }
  };

  useEffect(() => {
    GetVkAuthMode().then(mode => {
      setVkAuthMode(mode as 'anonymous' | 'account');
    }).catch(() => {});
    loadSubscriptions();
  }, []);

  // wdtt:// paste handler (only if linkMode is on).
  // Зависимость от settings.linkMode — если пользователь включит режим ссылки
  // после монтирования компонента, подписка будет установлена повторно.
  useEffect(() => {
    if (!settings.linkMode) return;
    return wdttLinkStore.subscribe((link) => {
      if (!link) return;
      const consumed = wdttLinkStore.consume();
      if (!consumed) return;
      const host = `${consumed.ip}:${consumed.dtlsPort}`;
      const name = consumed.name;
      const finish = async (saveHashes: boolean) => {
        const clean = consumed.hashes.map(stripVkUrl).filter(Boolean);
        const existing = serverStore.getAll().find(s => s.host === host);
        await SaveProfile(name, {
          peer: host, password: consumed.password, hashes: saveHashes ? clean : [],
          turn: '', port: '', device_id: '', listen: '',
        });
        const s = existing ?? serverStore.add({
          name, host, password: consumed.password,
          hashes: saveHashes ? (clean.slice(0, 4) as [string, string, string, string]) : ['', '', '', ''],
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
  }, [settings.linkMode]);

  const doConnect = async () => {
    const cur = selectedRef.current;
    if (!cur) return;
    const s = settingsStore.get();
    const useGlobal = cur.useGlobalHashes;
    const filled = (useGlobal ? s.hashes : cur.hashes).map(h => stripVkUrl(h)).filter(Boolean);
    if (filled.length === 0) {
      toastStore.show(useGlobal
        ? 'Добавьте глобальные хеши или заполните хеши профиля'
        : 'Заполните хеши VK в настройках сервера', 3500);
      return;
    }
    tunnelStore.set('connecting');
    const dnsUpstream = resolveDnsUpstream(s);
    try {
      if (vkAuthMode === 'account') {
        toastStore.show('Вход в VK...', 15000);
        await new Promise<void>((resolve, reject) => {
          const loginTimeout = setTimeout(() => {
            EventsOff('vk_login_done');
            reject(new Error('Таймаут входа в VK'));
          }, 60000);
          BackendVkLogin().catch(err => {
            clearTimeout(loginTimeout);
            EventsOff('vk_login_done');
            reject(err);
          });
          EventsOn('vk_login_done', (result: string) => {
            clearTimeout(loginTimeout);
            EventsOff('vk_login_done');
            if (result) resolve();
            else reject(new Error('Вход в VK не завершён'));
          });
        });
        toastStore.show('Получаю TURN креды VK...', 15000);
        await new Promise<void>((resolve, reject) => {
          const timeout = setTimeout(() => {
            EventsOff('vk_turn_creds');
            reject(new Error('Таймаут получения TURN кредов'));
          }, 30000);
          VkCallJoin(filled[0]).catch(err => {
            clearTimeout(timeout);
            EventsOff('vk_turn_creds');
            reject(err);
          });
          EventsOn('vk_turn_creds', (payload: string) => {
            clearTimeout(timeout);
            EventsOff('vk_turn_creds');
            if (payload) resolve();
            else reject(new Error('Не удалось получить TURN креды'));
          });
        });
      }
      // Загружаем список доменных исключений для текущей сессии.
      // Список хранится на бэкенде в файле, грузим при каждом подключении
      // чтобы подхватить изменения, сделанные через UI между сессиями.
      let excludeDomains: string[] = [];
      try {
        const list = await GetExcludeDomains();
        excludeDomains = Array.isArray(list) ? list : [];
      } catch {
        excludeDomains = [];
      }

      const params: backend.ConnectParams = {
        profile: cur.name,
        captchaMode: 'auto',
        vkAuthMode: vkAuthMode,
        workers: cur.power || 9,
        fingerprint: s.fingerprint,
        mtu: s.mtu || 1280,
        hashes: filled,
        obfsMode: 'audio',
        autoWG: s.autoWG,
        noDNSProxy: !s.dnsProxyEnabled,
        dnsUpstream: dnsUpstream.length > 0 ? dnsUpstream : undefined,
        wgInterface: s.wgInterface || 'WDTT',
        excludeDomains: excludeDomains.length > 0 ? excludeDomains : undefined,
        autoReconnect: s.autoReconnect !== false,
      };
      if (cur.subscriptionId) {
        // For subscription profiles pass runtime data so the backend does not
        // need a file in profiles/ (and avoids name collisions with manual profiles).
        params.peer = cur.host;
        params.password = cur.password;
        params.listen = cur.port ? `127.0.0.1:${cur.port}` : undefined;
      }
      await WailsConnect(params);
      navigate('/logs');
    } catch (err) {
      tunnelStore.set('idle');
      toastStore.show(
        err instanceof Error ? `Ошибка: ${err.message}` : 'Ошибка запуска туннеля',
        4000,
      );
    }
  };

  const handleConnect = async () => {
    if (tunnelStateRef.current === 'idle') {
      if (!selectedRef.current) {
        setAddServerOpen(true);
        return;
      }
      if (Date.now() < reconnectAt) {
        const secs = Math.ceil((reconnectAt - Date.now()) / 1000);
        toastStore.show(`Подождите ${secs} сек.`, 2000);
        return;
      }
      toastStore.show('Запускаю туннель', 2000);
      await doConnect();
    } else if (tunnelStateRef.current === 'connected' || tunnelStateRef.current === 'connecting' || tunnelStateRef.current === 'reconnecting') {
      tunnelStore.set('disconnecting');
      try {
        await WailsDisconnect();
      } catch {
        await ForceDisconnect();
      }
      tunnelStore.set('idle');
      setReconnectAt(Date.now() + 4000);
    } else {
      // stuck в disconnecting — force reset
      await ForceDisconnect();
      tunnelStore.set('idle');
    }
  };

  const handleAdd = (data: Omit<Server, 'id'>) => {
    const s = serverStore.add(data);
    setServers(serverStore.getAll());
    setSelected(s);
  };

  const syncSubscriptionProfiles = (subId: string, profs: Record<string, backend.ProfileData>) => {
    // Remove stale profiles from this subscription.
    const others = serverStore.getAll().filter(s => s.subscriptionId !== subId);

    for (const [name, p] of Object.entries(profs)) {
      const host = p.peer;
      if (!host) continue;
      const cleanHashes = (p.hashes || []).map(stripVkUrl).filter(Boolean);
      const port = p.port ? parseInt(p.port, 10) : undefined;
      const serverData: Omit<Server, 'id'> = {
        name,
        host,
        password: p.password || '',
        hashes: (cleanHashes.slice(0, 4) as [string, string, string, string]) || ['', '', '', ''],
        useGlobalHashes: cleanHashes.length === 0,
        power: 9,
        subscriptionId: subId,
        port: port && !isNaN(port) ? port : undefined,
      };
      const existing = others.find(s => s.host === host && s.subscriptionId === subId);
      if (existing) {
        const idx = others.findIndex(s => s.id === existing.id);
        others[idx] = { ...existing, ...serverData };
      } else {
        const id = crypto.randomUUID();
        others.push({ ...serverData, id });
      }
    }

    serverStore.save(others);
    setServers(others);
  };

  const handleSubscriptionSync = async (id: string) => {
    try {
      await UpdateSubscription(id);
      const profs = await GetSubscriptionProfiles();
      syncSubscriptionProfiles(id, profs);
      toastStore.show('Подписка обновлена', 2500);
      await loadSubscriptions();
    } catch (e) {
      toastStore.show(e instanceof Error ? e.message : 'Ошибка обновления подписки', 4000);
    }
  };

  const handleSubscriptionDelete = async (id: string) => {
    if (!window.confirm('Удалить подписку и все её профили?')) return;
    try {
      await DeleteSubscription(id);
      // Remove all local profiles tied to this subscription.
      const remaining = serverStore.getAll().filter(s => s.subscriptionId !== id);
      serverStore.save(remaining);
      setServers(remaining);
      if (selected?.subscriptionId === id) {
        setSelected(remaining[0] ?? null);
      }
      toastStore.show('Подписка удалена', 2500);
      await loadSubscriptions();
    } catch (e) {
      toastStore.show(e instanceof Error ? e.message : 'Ошибка удаления', 4000);
    }
  };

  const handleSubscriptionFolder = async (id: string) => {
    try {
      await OpenSubscriptionFolder(id);
    } catch {
      toastStore.show('Не удалось открыть папку', 2500);
    }
  };

  const handleImportQwdtt = async (result: { profiles: Array<{ name: string; peer: string; hashes: string[]; workers: number; password: string }>; groupName?: string }) => {
    let count = 0;
    for (const p of result.profiles) {
      const name = result.groupName
        ? `${result.groupName} - ${p.name}`
        : p.name;
      const cleanHashes = p.hashes.map(stripVkUrl).filter(Boolean);
      await SaveProfile(name, {
        peer: p.peer,
        password: p.password,
        hashes: cleanHashes,
        turn: '', port: '', device_id: '', listen: '',
      }).catch(() => {});
      serverStore.add({
        name,
        host: p.peer,
        password: p.password,
        hashes: (cleanHashes.slice(0, 4) as [string, string, string, string]).length > 0
          ? (cleanHashes.slice(0, 4) as [string, string, string, string])
          : ['', '', '', ''],
        useGlobalHashes: cleanHashes.length === 0,
        power: p.workers || 9,
      });
      count++;
    }
    setServers(serverStore.getAll());
    toastStore.show(`Импортировано профилей: ${count}`, 3000);
  };

  const handleApplyLink = async (link: { ip: string; dtlsPort: string; password: string; hashes: string[]; name: string }) => {
    const host = `${link.ip}:${link.dtlsPort}`;
    const name = link.name;
    const clean = link.hashes.map(stripVkUrl).filter(Boolean);
    const existing = serverStore.getAll().find(s => s.host === host);
    await SaveProfile(name, {
      peer: host, password: link.password, hashes: clean,
      turn: '', port: '', device_id: '', listen: '',
    });
    const s = existing ?? serverStore.add({
      name, host, password: link.password,
      hashes: (clean.slice(0, 4) as [string, string, string, string]).length > 0
        ? (clean.slice(0, 4) as [string, string, string, string])
        : ['', '', '', ''],
      useGlobalHashes: clean.length === 0,
      power: 9,
    });
    setServers(serverStore.getAll());
    setSelected(s);
    if (clean.length > 0) {
      toastStore.show(`Профиль создан + ${clean.length} хешей`, 3000);
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
        .tn-profiles { display: flex; flex-direction: column; gap: 18px; margin: 0 16px; }
        .tn-profiles-head { display: flex; align-items: center; justify-content: space-between; padding: 0 2px; }
        .tn-profiles-title { font-size: 12px; font-weight: 600; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.5px; }
        .tn-profiles-count { font-size: 12px; color: var(--text-4); }
        .tn-profiles-row { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; padding: 2px 2px 6px; }
        .tn-sub-group { display: flex; flex-direction: column; gap: 14px; }
        .tn-profiles-row--sub { padding-left: 0; margin-left: 0; }
        .tn-pcard { background: var(--surface); border: 1.5px solid var(--border); border-radius: var(--r-card); padding: 12px 14px; cursor: pointer; display: flex; flex-direction: column; gap: 6px; transition: border-color 0.12s, background 0.12s, transform 0.12s; position: relative; }
        .tn-pcard:hover { border-color: var(--text-3); transform: translateY(-1px); }
        .tn-pcard--active { border-color: var(--accent); background: var(--accent-soft); }
        .tn-pcard-head { display: flex; align-items: center; gap: 8px; }
        .tn-pcard-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-4); flex-shrink: 0; }
        .tn-pcard--active .tn-pcard-dot { background: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
        .tn-pcard-name { flex: 1; font-size: 14px; font-weight: 600; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .tn-pcard-edit { background: none; border: none; cursor: pointer; padding: 2px; color: var(--text-3); display: flex; border-radius: 6px; transition: background 0.12s, color 0.12s; }
        .tn-pcard-edit:hover { background: var(--bg-2); color: var(--accent); }
        .tn-pcard-host { font-family: 'Geist Mono', monospace; font-size: 11px; color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .tn-pcard-foot { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-3); }
        .tn-pcard-power { color: var(--accent); font-weight: 600; padding: 1px 6px; border-radius: 4px; background: var(--accent-soft); }
        .tn-pcard-hashes { display: flex; align-items: center; gap: 3px; }
        .tn-pcard-hashes--full { color: var(--accent); font-weight: 600; }
        .tn-pcard-add { background: transparent; border: 1.5px dashed var(--border); border-radius: var(--r-card); padding: 12px 14px; cursor: pointer; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; color: var(--text-3); font-size: 13px; font-weight: 500; transition: border-color 0.12s, color 0.12s, background 0.12s; }
        .tn-pcard-add:hover { border-color: var(--accent); color: var(--accent); background: var(--accent-soft); }
        .tn-sub-card { background: var(--surface); border: 1.5px solid var(--border); border-radius: var(--r-card); padding: 14px 16px; display: flex; flex-direction: column; gap: 8px; }
        .tn-sub-head { display: flex; align-items: center; gap: 10px; }
        .tn-sub-name { flex: 1; font-size: 14px; font-weight: 600; color: var(--text); }
        .tn-sub-actions { display: flex; align-items: center; gap: 6px; }
        .tn-sub-btn { background: var(--bg-2); border: 1px solid var(--border); border-radius: 6px; padding: 6px; cursor: pointer; color: var(--text-3); display: flex; transition: background 0.12s, color 0.12s; }
        .tn-sub-btn:hover { background: var(--accent-soft); color: var(--accent); }
        .tn-sub-btn--danger:hover { background: var(--danger-soft); color: var(--danger); }
        .tn-sub-desc { font-size: 12px; color: var(--text-3); }
        .tn-sub-traffic { display: flex; align-items: center; gap: 10px; }
        .tn-sub-bar { flex: 1; height: 6px; background: var(--bg-3); border-radius: var(--r-toggle); overflow: hidden; }
        .tn-sub-bar-fill { height: 100%; background: var(--accent); border-radius: var(--r-toggle); }
        .tn-sub-traffic-text { font-size: 11px; color: var(--text-3); font-family: 'Geist Mono', monospace; white-space: nowrap; }
        .tn-sub-error { font-size: 11px; color: var(--danger); }
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
          <div className="tn-divider" />
          <button
            className="tn-link-row"
            onClick={() => setImportQwdttOpen(true)}
            type="button"
          >
            <IconFileImport size={16} style={{ color: 'var(--text-3)' }} />
            <span className="tn-link-row-label">Импорт .qwdtt</span>
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
            className={`tn-action ${tunnelState === 'connected' || tunnelState === 'connecting' || tunnelState === 'reconnecting' ? 'tn-action--danger' : 'tn-action--filled'}`}
            onClick={handleConnect}
          >
            {tunnelState === 'connected' || tunnelState === 'connecting' || tunnelState === 'reconnecting' ? (
              <>
                <IconPlayerStopFilled size={18} />
                {tunnelState === 'connected' ? 'Отключить' : TUNNEL_LABEL[tunnelState]}
              </>
            ) : (
              <>
                <IconBolt size={18} />
                {TUNNEL_LABEL[tunnelState] === 'Отключение…' ? 'Отключение…' : 'Подключить'}
              </>
            )}
          </button>
        </div>

        {/* PROFILES: manual profiles first */}
        {(manualServers.length > 0 || subscriptions.length > 0) && (
          <div className="tn-profiles">
            {manualServers.length > 0 && (
              <>
                <div className="tn-profiles-head">
                  <span className="tn-profiles-title">Сохранённые профили</span>
                  <span className="tn-profiles-count">{manualServers.length} шт.</span>
                </div>
                <div className="tn-profiles-row">
                  {manualServers.map(s => {
                    const filled = (s.useGlobalHashes ? settings.hashes : s.hashes).filter(h => h.trim()).length;
                    const isActive = selected?.id === s.id;
                    return (
                      <div
                        key={s.id}
                        className={`tn-pcard${isActive ? ' tn-pcard--active' : ''}`}
                        onClick={() => setSelected(s)}
                      >
                        <div className="tn-pcard-head">
                          <span className="tn-pcard-dot" />
                          <span className="tn-pcard-name">{s.name}</span>
                          <button
                            className="tn-pcard-edit"
                            onClick={e => { e.stopPropagation(); setEditServer(s); }}
                            title="Изменить"
                          >
                            <IconPencil size={14} />
                          </button>
                        </div>
                        <div className="tn-pcard-host">{s.host}</div>
                        <div className="tn-pcard-foot">
                          <span className="tn-pcard-power">{s.power}w</span>
                          <span className={`tn-pcard-hashes${filled === 4 ? ' tn-pcard-hashes--full' : ''}`}>
                            {filled === 4 && <IconCheck size={11} />}
                            {filled}/4 хешей
                          </span>
                        </div>
                      </div>
                    );
                  })}
                  <button
                    className="tn-pcard-add"
                    onClick={() => setAddServerOpen(true)}
                  >
                    <IconPlus size={20} />
                    Добавить
                  </button>
                </div>
              </>
            )}

            {/* Subscriptions with their profiles */}
            {subscriptions.map(sub => {
              const subServers = servers.filter(s => s.subscriptionId === sub.id);
              const trafficPct = sub.trafficLimitMb && sub.trafficLimitMb > 0
                ? Math.min(100, Math.round((sub.trafficUsedMb || 0) / sub.trafficLimitMb * 100))
                : 0;
              return (
                <div key={sub.id} className="tn-sub-group">
                  <div className="tn-sub-card">
                    <div className="tn-sub-head">
                      <IconWorld size={16} style={{ color: 'var(--accent)' }} />
                      <span className="tn-sub-name">{sub.name}</span>
                      <div className="tn-sub-actions">
                        <button className="tn-sub-btn" onClick={() => handleSubscriptionSync(sub.id)} title="Обновить">
                          <IconRotate size={14} />
                        </button>
                        <button className="tn-sub-btn" onClick={() => handleSubscriptionFolder(sub.id)} title="Открыть папку">
                          <IconFolder size={14} />
                        </button>
                        <button className="tn-sub-btn tn-sub-btn--danger" onClick={() => handleSubscriptionDelete(sub.id)} title="Удалить">
                          <IconTrash size={14} />
                        </button>
                      </div>
                    </div>
                    {sub.description && <div className="tn-sub-desc">{sub.description}</div>}
                    {sub.trafficLimitMb && sub.trafficLimitMb > 0 && (
                      <div className="tn-sub-traffic">
                        <div className="tn-sub-bar">
                          <div className="tn-sub-bar-fill" style={{ width: `${trafficPct}%` }} />
                        </div>
                        <span className="tn-sub-traffic-text">{Math.round(sub.trafficUsedMb || 0)} / {Math.round(sub.trafficLimitMb)} МБ</span>
                      </div>
                    )}
                    {sub.lastSyncError && <div className="tn-sub-error">{sub.lastSyncError}</div>}
                  </div>

                  {subServers.length > 0 && (
                    <div className="tn-profiles-row tn-profiles-row--sub">
                      {subServers.map(s => {
                        const filled = (s.useGlobalHashes ? settings.hashes : s.hashes).filter(h => h.trim()).length;
                        const isActive = selected?.id === s.id;
                        return (
                          <div
                            key={s.id}
                            className={`tn-pcard${isActive ? ' tn-pcard--active' : ''}`}
                            onClick={() => setSelected(s)}
                          >
                            <div className="tn-pcard-head">
                              <span className="tn-pcard-dot" />
                              <span className="tn-pcard-name">{s.name}</span>
                            </div>
                            <div className="tn-pcard-host">{s.host}</div>
                            <div className="tn-pcard-foot">
                              <span className="tn-pcard-power">{s.power}w</span>
                              <span className={`tn-pcard-hashes${filled === 4 ? ' tn-pcard-hashes--full' : ''}`}>
                                {filled === 4 && <IconCheck size={11} />}
                                {filled}/4 хешей
                              </span>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}

            <button
              className="tn-pcard-add"
              onClick={() => setAddSubOpen(true)}
            >
              <IconWorld size={20} />
              Добавить подписку
            </button>
          </div>
        )}
      </div>

      {addServerOpen && <AddServer onClose={() => setAddServerOpen(false)} onAdd={handleAdd} />}
      {addSubOpen && (
        <AddSubscriptionModal
          onClose={() => setAddSubOpen(false)}
          onAdded={async () => {
            await loadSubscriptions();
            setServers(serverStore.getAll());
          }}
        />
      )}
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
      {importQwdttOpen && (
        <ImportQwdtt
          onClose={() => setImportQwdttOpen(false)}
          onImport={handleImportQwdtt}
        />
      )}
    </>
  );
}

