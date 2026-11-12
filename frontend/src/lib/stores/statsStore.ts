import type { Dashboard } from '../types';

// Отдельный канал статистики (U8): дашборд не смешивается с логами.
// Бэкенд эмитит событие "stats" раз в секунду; стор хранит последний снимок,
// чтобы при переходе на страницу «Туннель» данные появились мгновенно.

type Listener = (d: Dashboard) => void;

let current: Dashboard | null = null;
const listeners = new Set<Listener>();

export const statsStore = {
  subscribe: (fn: Listener) => {
    listeners.add(fn);
    if (current) fn(current);
    return () => { listeners.delete(fn); };
  },
  set: (d: Dashboard) => {
    current = d;
    listeners.forEach(fn => fn(d));
  },
  get: (): Dashboard | null => current,
};
