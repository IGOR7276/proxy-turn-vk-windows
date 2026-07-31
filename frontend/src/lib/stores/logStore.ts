export type LogLevel = 'INFO' | 'ERROR' | 'WARN' | 'DEBUG';

interface InternalEntry {
  id: number;
  level: LogLevel;
  message: string;
  time: string;
  count: number;
  key?: string;
  priority?: number;
}

type Listener = (entries: InternalEntry[]) => void;

let seq = 0;
let entries: InternalEntry[] = [];
const listeners = new Set<Listener>();
const MAX_ENTRIES = 500;

function notify() {
  listeners.forEach(fn => fn([...entries]));
}

function assignKey(message: string, level: LogLevel): { key?: string; priority?: number } {
  if (message.includes('[CORE] Воркеров:') || /Воркеров:\s*\d+/.test(message)) {
    return { key: '__workers__', priority: 0 };
  }
  if (message.includes('WRAP') || message.includes('RTP') || message.includes('AEAD')) {
    return { key: 'wrap', priority: 1 };
  }
  if (message.includes('Handshake') || message.includes('Рукопожатие') || message.includes('рукопожатие')) {
    return { key: 'dtls', priority: 1 };
  }
  if (message.includes('[WG] Конфиг применён') || message.includes('туннель активен')) {
    return { key: 'ready', priority: 1 };
  }
  if (message.includes('[VK Auth]') && (message.includes('токен') || message.includes('получен') || message.includes('OAuth') || message.includes('креды') || message.includes('TURN'))) {
    return { key: 'creds_ok', priority: 2 };
  }
  if (message.includes('[VK Auth]') || message.includes('[VK]') || message.includes('учетные данные') || message.includes('Учетные')) {
    return { key: 'creds', priority: 2 };
  }
  if (message.includes('[TURN]')) {
    return { key: 'turn', priority: 2 };
  }
  if (message.includes('[СОСТОЯНИЕ]')) {
    return { key: 'state', priority: 1 };
  }
  if (message.startsWith('[СХЕМА]')) {
    return { key: 'pipeline_error', priority: 1 };
  }
  if (message.includes('[ОШИБКА]')) {
    return { key: 'error_' + Math.random().toString(36).slice(2, 6), priority: 99 };
  }
  if (message.startsWith('[СТАТИСТИКА]') || message.startsWith('[STATISTICS]')) {
    return { key: '__stats__', priority: 3 };
  }
  if (message.includes('[КАПЧА AUTO]') && (message.includes('старт') || message.includes('цепоч'))) {
    return { key: 'captcha_start', priority: 5 };
  }
  if (message.includes('[КАПЧА AUTO]') && (message.includes('решил') || message.includes('Go') || message.includes('WBV'))) {
    return { key: 'captcha_solve', priority: 5 };
  }
  if (message.includes('[КАПЧА]') || message.includes('Решение капчи') || message.includes('[CAPTCHA]')) {
    return { key: 'captcha', priority: 5 };
  }
  if (level === 'ERROR') {
    return { priority: 50 };
  }
  return {};
}

export const logStore = {
  subscribe: (fn: Listener) => {
    listeners.add(fn);
    fn([...entries]);
    return () => { listeners.delete(fn); };
  },

  push: (level: LogLevel, message: string) => {
    const time = new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    const { key, priority } = assignKey(message, level);

    if (key) {
      entries = [...entries, { id: seq++, level, message, time, count: 1, key, priority }];
      if (entries.length > MAX_ENTRIES) entries = entries.slice(-MAX_ENTRIES);
      notify();
      return;
    }

    const entry: InternalEntry = { id: seq++, level, message, time, count: 1, key, priority };
    entries = [...entries, entry];
    if (entries.length > MAX_ENTRIES) entries = entries.slice(-MAX_ENTRIES);
    notify();
  },

  pushKeyed: (level: LogLevel, message: string, key: string, priority?: number) => {
    const time = new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    entries = [...entries, { id: seq++, level, message, time, count: 1, key, priority }];
    if (entries.length > MAX_ENTRIES) entries = entries.slice(-MAX_ENTRIES);
    notify();
  },

  clear: () => {
    entries = [];
    notify();
  },

  getAll: () => [...entries],
};

export type LogEntry = InternalEntry;
