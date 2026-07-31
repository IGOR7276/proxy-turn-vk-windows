export type ConnectionStep = 'dns' | 'vk' | 'captcha' | 'wrap' | 'turn' | 'dtls' | 'workers' | 'wg' | 'done';

export interface PipelineState {
  visible: boolean;
  current: ConnectionStep | null;
  completed: ConnectionStep[];
  failed: ConnectionStep | null;
  timedOut: boolean;
  timeoutSec: number;
}

const STEP_ORDER: ConnectionStep[] = ['dns', 'vk', 'captcha', 'wrap', 'turn', 'dtls', 'workers', 'wg', 'done'];

const STEP_LABEL: Record<ConnectionStep, string> = {
  dns: 'DNS',
  vk: 'VK',
  captcha: 'Капча',
  wrap: 'WRAP',
  turn: 'TURN',
  dtls: 'DTLS',
  workers: 'Потоки',
  wg: 'VPN',
  done: 'OK',
};

const STEP_DETAIL: Record<ConnectionStep, string> = {
  dns: 'Проверка DNS до VK',
  vk: 'Получение TURN-кредов',
  captcha: 'Решение Smart Captcha',
  wrap: 'RTP-маскировка трафика',
  turn: 'Relay через VK',
  dtls: 'Handshake с сервером',
  workers: 'Активация воркеров',
  wg: 'Запуск WireGuard',
  done: 'Туннель работает',
};

type Listener = (state: PipelineState) => void;

let state: PipelineState = {
  visible: false,
  current: null,
  completed: [],
  failed: null,
  timedOut: false,
  timeoutSec: 0,
};

const listeners = new Set<Listener>();

function notify() {
  listeners.forEach(fn => fn({ ...state }));
}

export const pipelineStore = {
  subscribe: (fn: Listener) => {
    listeners.add(fn);
    fn({ ...state });
    return () => { listeners.delete(fn); };
  },

  reset: () => {
    state = {
      visible: true,
      current: 'dns',
      completed: [],
      failed: null,
      timedOut: false,
      timeoutSec: 0,
    };
    notify();
  },

  hide: () => {
    state = { ...state, visible: false };
    notify();
  },

  setFromBackend: (payload: PipelineState) => {
    state = { ...payload };
    notify();
  },

  getStepsToShow: (s: PipelineState): ConnectionStep[] => {
    const base: ConnectionStep[] = ['dns', 'vk', 'wrap', 'turn', 'dtls', 'workers', 'wg'];
    if (s.completed.includes('done') || s.current === 'done') return [...base, 'done'];
    return base;
  },

  stepLabel: (step: ConnectionStep) => STEP_LABEL[step],
  stepDetail: (step: ConnectionStep) => STEP_DETAIL[step],
  stepOrder: (step: ConnectionStep) => STEP_ORDER.indexOf(step),

  currentDetail: (s: PipelineState): string => {
    if (s.failed) {
      return s.timedOut
        ? `Таймаут ${s.timeoutSec} с: ${STEP_LABEL[s.failed]}`
        : `Ошибка: ${STEP_DETAIL[s.failed]}`;
    }
    if (s.current) return STEP_DETAIL[s.current];
    if (s.completed.includes('done')) return STEP_DETAIL.done;
    return 'Ожидание…';
  },
};
