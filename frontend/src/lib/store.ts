import type { AppSettings, AppState, DeployConfig, Server } from './types';
import { DEFAULT_SETTINGS, DEFAULT_DEPLOY } from './types';
import {
  GetAppState,
  ImportLegacyState,
  SaveServers as WailsSaveServers,
  DeleteServer as WailsDeleteServer,
  SelectServer as WailsSelectServer,
  SaveSettings as WailsSaveSettings,
  SaveDeployConfig as WailsSaveDeployConfig,
} from '../../wailsjs/go/backend/App';

// Q1: единственный источник правды — бэкенд. Модуль держит синхронный кэш,
// который гидратируется из GetAppState() при старте; записи сразу обновляют
// кэш и асинхронно уходят в бэкенд. localStorage читается только для
// одноразовой миграции старых установок.

const SERVERS_KEY = 'wdtt_servers';
const SETTINGS_KEY = 'wdtt_settings';
const SELECTED_KEY = 'wdtt_selected_server';
const DEPLOY_KEY = 'wdtt_deploy';

function parse<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

export function normalizeServer(s: Server): Server {
  const h = Array.isArray(s.hashes) ? s.hashes : [];
  return {
    ...s,
    hashes: [h[0] ?? '', h[1] ?? '', h[2] ?? '', h[3] ?? ''],
    useGlobalHashes: s.useGlobalHashes ?? true,
    power: typeof s.power === 'number' && s.power > 0 ? s.power : 9,
    subscriptionId: s.subscriptionId || undefined,
    port: typeof s.port === 'number' ? s.port : undefined,
  };
}

function normalizeSettings(s: AppSettings): AppSettings {
  const merged = { ...DEFAULT_SETTINGS, ...s };
  const h = Array.isArray(merged.hashes) ? merged.hashes : [];
  merged.hashes = [h[0] ?? '', h[1] ?? '', h[2] ?? '', h[3] ?? ''];
  return merged;
}

function readLegacySettings(): AppSettings {
  const saved = parse<Partial<AppSettings> & { dnsUpstream?: string }>(SETTINGS_KEY, {});
  const merged = normalizeSettings({ ...DEFAULT_SETTINGS, ...saved });
  // миграция со старого формата: dnsUpstream="8.8.8.8,1.1.1.1" → dnsProvider='google'
  if (typeof saved.dnsUpstream === 'string' && !saved.dnsProvider) {
    const legacy = saved.dnsUpstream.replace(/\s+/g, '');
    if (legacy === '8.8.8.8,8.8.4.4') merged.dnsProvider = 'google';
    else if (legacy === '1.1.1.1,1.0.0.1') merged.dnsProvider = 'cloudflare';
    else if (legacy === '77.88.8.8,77.88.8.1') merged.dnsProvider = 'yandex';
    else { merged.dnsProvider = 'custom'; merged.dnsCustom = legacy; }
  }
  return merged;
}

// ─── Кэш и очередь записи ───

let hydrated = false;
let serversCache: Server[] | null = null;
let settingsCache: AppSettings | null = null;
let deployCache: DeployConfig | null = null;
let selectedCache: string | null | undefined;

// Все записи сериализуются: иначе быстрые правки полей могут прийти в бэкенд
// в обратном порядке и затереть более новое значение старым.
let queue: Promise<unknown> = Promise.resolve();
function enqueue(fn: () => Promise<unknown>) {
  queue = queue.then(fn).catch(err => console.error('[store] backend write failed:', err));
}

/** hydrateAppState вызывается один раз при старте приложения. */
export async function hydrateAppState(): Promise<AppState> {
  let state = (await GetAppState()) as unknown as AppState;

  if (state.legacyMigrationPending) {
    const legacyServers = parse<Server[]>(SERVERS_KEY, []).map(normalizeServer);
    const legacySettings = readLegacySettings();
    const legacyDeploy = parse<DeployConfig>(DEPLOY_KEY, DEFAULT_DEPLOY);
    const legacySelected = localStorage.getItem(SELECTED_KEY) ?? '';
    await ImportLegacyState(legacyServers, legacySettings, legacyDeploy, legacySelected);
    state = (await GetAppState()) as unknown as AppState;
    // Старые ключи больше не нужны: источник правды — бэкенд.
    localStorage.removeItem(SERVERS_KEY);
    localStorage.removeItem(SETTINGS_KEY);
    localStorage.removeItem(SELECTED_KEY);
    localStorage.removeItem(DEPLOY_KEY);
  }

  applyAppState(state);
  hydrated = true;
  return state;
}

function applyAppState(state: AppState) {
  serversCache = (state.servers ?? []).map(normalizeServer);
  settingsCache = normalizeSettings(state.settings ?? DEFAULT_SETTINGS);
  deployCache = { ...DEFAULT_DEPLOY, ...(state.deploy ?? {}) };
  selectedCache = state.selectedServer || null;
}

export function isHydrated() {
  return hydrated;
}

// ─── Серверы ───

export const serverStore = {
  getAll: (): Server[] => {
    if (serversCache) return serversCache.map(normalizeServer);
    return parse<Server[]>(SERVERS_KEY, []).map(normalizeServer);
  },
  save: (servers: Server[]) => {
    const payload = servers.map(normalizeServer);
    serversCache = payload;
    enqueue(() => WailsSaveServers(payload));
  },
  add: (server: Omit<Server, 'id'>): Server => {
    const s: Server = normalizeServer({ ...server, id: crypto.randomUUID() } as Server);
    serverStore.save([...serverStore.getAll(), s]);
    return s;
  },
  update: (server: Server) => {
    const s = normalizeServer(server);
    serverStore.save(serverStore.getAll().map(x => x.id === s.id ? s : x));
  },
  remove: (id: string) => {
    const all = serverStore.getAll().filter(s => s.id !== id);
    serverStore.save(all);
    if (selectionStore.get() === id) {
      selectionStore.set(all[0]?.id ?? null);
    }
    enqueue(() => WailsDeleteServer(id));
  },
};

export const selectionStore = {
  get: (): string | null => {
    if (selectedCache !== undefined) return selectedCache;
    const v = localStorage.getItem(SELECTED_KEY);
    return v && v !== 'null' ? v : null;
  },
  set: (id: string | null) => {
    selectedCache = id;
    enqueue(() => WailsSelectServer(id ?? ''));
  },
};

// ─── Настройки ───

export const settingsStore = {
  get: (): AppSettings => {
    if (settingsCache) return normalizeSettings(settingsCache);
    return readLegacySettings();
  },
  save: (settings: AppSettings) => {
    const payload = normalizeSettings(settings);
    settingsCache = payload;
    enqueue(() => WailsSaveSettings(payload));
  },
};

// ─── Деплой ───

export const deployStore = {
  get: (): DeployConfig => {
    if (deployCache) return { ...deployCache };
    return parse<DeployConfig>(DEPLOY_KEY, DEFAULT_DEPLOY);
  },
  save: (cfg: DeployConfig) => {
    const payload = { ...cfg };
    deployCache = payload;
    enqueue(() => WailsSaveDeployConfig(payload));
  },
};

// Экспорт для обратной совместимости с кодом, который ждал синхронные геттеры.
export { DEFAULT_SETTINGS, DEFAULT_DEPLOY };
export type { Server, AppSettings, DeployConfig };
