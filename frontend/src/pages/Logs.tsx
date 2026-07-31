import React, { useState, useEffect, useRef, useCallback } from 'react';
import { IconSearch, IconTrashX, IconCopy, IconTerminal2, IconCheck, IconX } from '@tabler/icons-react';
import { logStore, type LogEntry, type LogLevel } from '../lib/stores/logStore';
import { pipelineStore, type PipelineState } from '../lib/stores/pipelineStore';

type Filter = 'ALL' | 'MINI' | 'INFO' | 'ERROR';

const LEVEL_COLOR: Record<LogLevel, string> = {
  INFO:  'var(--text)',
  WARN:  '#f59e0b',
  ERROR: '#ef4444',
  DEBUG: 'var(--text-3)',
};

const MINI_COLORS: Record<string, string> = {
  __workers__: '#4CAF50',
  __wg__: '#4CAF50',
  wrap: '#4CAF50',
  dtls: '#4CAF50',
  ready: '#4CAF50',
  creds: '#4CAF50',
  creds_ok: '#4CAF50',
  turn: '#4CAF50',
  state: '#4CAF50',
  __stats__: '#42A5F5',
  captcha_start: 'var(--text)',
  captcha_solve: 'var(--text)',
  captcha: 'var(--text)',
};

function miniEntry(entry: LogEntry): LogEntry {
  const msg = entry.message;
  const kv = entry.key || '';

  if (kv === '__workers__' || msg.includes('Воркеров:')) {
    const g = msg.match(/групп:\s*(\d+)/);
    const w = msg.match(/по\s*(\d+)/);
    const groups = g ? g[1] : '?';
    const perGroup = w ? w[1] : '?';
    return { ...entry, message: `[Основной] Хешей=${groups}, Потоков=${+groups * +perGroup}` };
  }
  if (kv === 'wrap') {
    return { ...entry, message: `[WRAP] Ключ выведен из пароля, режим RTP AEAD активен` };
  }
  if (kv === 'dtls') {
    return { ...entry, message: `[DTLS] Рукопожатие (Handshake)...` };
  }
  if (kv === 'ready') {
    return { ...entry, message: `[READY] Туннель активен` };
  }
  if (kv === 'creds_ok') {
    return { ...entry, message: `[ВК] Учетные данные проверены \u2713` };
  }
  if (kv === 'creds') {
    return { ...entry, message: `[ВК] Получение данных...` };
  }
  if (kv === 'turn') {
    const cleaned = msg.replace(/^\[TURN\] /, '');
    return { ...entry, message: `[TURN] ${cleaned}` };
  }
  if (kv === '__stats__') {
    return { ...entry, message: `[СТАТИСТИКА] ${msg.replace(/^\[СТАТИСТИКА\]\s*/, '').replace(/^\[STATISTICS\]\s*/, '')}` };
  }
  if (kv === 'captcha_start') {
    return { ...entry, message: `[КАПЧА AUTO] старт цепочки` };
  }
  if (kv === 'captcha_solve') {
    return { ...entry, message: `[КАПЧА AUTO] Go v2 решил капчу` };
  }
  if (kv === 'captcha') {
    return { ...entry, message: `[КАПЧА] Решение капчи...` };
  }
  if (kv === 'state') {
    const cleaned = msg.replace(/^\[СОСТОЯНИЕ\] /, '');
    return { ...entry, message: `[Состояние] ${cleaned}` };
  }
  if (msg.startsWith('[СХЕМА]')) {
    return { ...entry, message: msg };
  }
  if (kv === 'pipeline_error') {
    return { ...entry, message: msg };
  }
  // fallback
  const cleaned = msg.replace(/^\[CORE\] /, '').replace(/^\[STREAM \d+\] /, '');
  return { ...entry, message: `[${cleaned}]` };
}

export default function Logs() {
  const [filter, setFilter] = useState<Filter>('MINI');
  const [search, setSearch] = useState('');
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [pipeline, setPipeline] = useState<PipelineState>({ visible: false, current: null, completed: [], failed: null, timedOut: false, timeoutSec: 0 });
  const bottomRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const autoScroll = useRef(true);

  useEffect(() => logStore.subscribe(setEntries), []);
  useEffect(() => pipelineStore.subscribe(setPipeline), []);

  useEffect(() => {
    if (autoScroll.current && filter !== 'MINI') bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [entries, filter]);

  const onScroll = useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }, []);

  const miniEntries = (() => {
    const map = new Map<string, LogEntry & { lastIdx: number }>();
    entries
      .filter(e => (e.priority ?? 99) < 10 && e.key)
      .map(miniEntry)
      .forEach((e, idx) => {
        const existing = map.get(e.key!);
        if (existing) {
          existing.count += 1;
          existing.message = e.message;
          existing.time = e.time;
          existing.lastIdx = idx;
        } else {
          map.set(e.key!, { ...e, count: 1, lastIdx: idx });
        }
      });
    return [...map.values()].sort((a, b) => a.lastIdx - b.lastIdx);
  })();

  const visible = (filter === 'MINI' ? miniEntries : entries).filter(e => {
    if (filter !== 'ALL' && filter !== 'MINI' && e.level !== filter) return false;
    if (search && !e.message.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const handleCopy = () => {
    const text = visible.map(e => e.message).join('\n');
    navigator.clipboard.writeText(text);
  };

  return (
    <>
      <style>{`
        .lg-wrap { display: flex; flex-direction: column; gap: 18px; animation: page-in 0.3s ease-out; height: calc(100vh - 140px); }
        .lg-header { display: flex; align-items: center; gap: 10px; padding: 4px 4px 0; flex-shrink: 0; }
        .lg-title { font-size: 20px; font-weight: 600; color: var(--text); flex: 1; }
        .lg-card { flex: 1; min-height: 0; background: var(--surface); border: 1px solid var(--border); border-radius: var(--r-card); display: flex; flex-direction: column; overflow: hidden; box-shadow: var(--shadow); }
        .lg-toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 18px; border-bottom: 1px solid var(--border-2); flex-shrink: 0; }
        .lg-search-wrap { flex: 1; position: relative; max-width: 380px; }
        .lg-search-input { width: 100%; padding: 9px 38px 9px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); font-size: 14px; color: var(--text); outline: none; box-sizing: border-box; font-family: 'Geist', sans-serif; }
        .lg-search-input::placeholder { color: var(--text-4); }
        .lg-search-input:focus { border-color: var(--accent); }
        .lg-search-icon { position: absolute; right: 12px; top: 50%; transform: translateY(-50%); color: var(--text-3); pointer-events: none; }
        .lg-toolbar-right { display: flex; align-items: center; gap: 8px; flex-shrink: 0; margin-left: auto; }
        .lg-seg { display: flex; background: var(--seg-bg); border-radius: var(--r-pill); padding: 3px; gap: 2px; }
        .lg-seg-btn { padding: 7px 14px; border: none; border-radius: var(--r-pill); font-size: 12px; font-weight: 600; cursor: pointer; background: transparent; color: var(--seg-text); transition: background 0.15s, color 0.15s; font-family: 'Geist', sans-serif; }
        .lg-seg-btn--active { background: var(--accent); color: var(--accent-fg); }
        .lg-icon-btn { width: 36px; height: 36px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); cursor: pointer; display: flex; align-items: center; justify-content: center; color: var(--text-3); transition: background 0.12s, color 0.12s; }
        .lg-icon-btn:hover { background: var(--bg-2); color: var(--text); }
        .lg-list { flex: 1; min-height: 0; overflow-y: auto; padding: 8px 0; }
        .lg-row { display: flex; align-items: baseline; gap: 10px; padding: 5px 18px; font-size: 13px; line-height: 1.5; font-family: 'Geist Mono', monospace; }
        .lg-row:hover { background: var(--bg-2); }
        .lg-time { color: var(--text-4); flex-shrink: 0; font-size: 12px; font-variant-numeric: tabular-nums; }
        .lg-level { flex-shrink: 0; font-weight: 700; font-size: 12px; width: 48px; }
        .lg-msg { flex: 1; word-break: break-all; color: var(--text); }
        .lg-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: var(--text-4); font-size: 14px; }
        .lg-empty svg { color: var(--text-4); }
        .lg-mini-row { display: flex; align-items: center; gap: 10px; padding: 5px 18px; font-size: 13px; line-height: 1.5; border-bottom: 1px solid var(--border-2); font-family: 'Geist Mono', monospace; }
        .lg-mini-arrow { color: var(--accent); flex-shrink: 0; opacity: 0.5; }
        .lg-mini-msg { flex: 1; word-break: break-all; font-weight: 500; }
        .lg-mini-badge { flex-shrink: 0; background: rgba(66, 165, 245, 0.15); border-radius: 20px; padding: 1px 8px; font-size: 11px; color: #42A5F5; }
        .lg-pipeline { flex-shrink: 0; background: var(--accent-soft); border-bottom: 1px solid var(--border-2); padding: 14px 18px; }
        .lg-pipeline-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
        .lg-pipeline-title { font-size: 12px; font-weight: 600; color: var(--text); }
        .lg-pipeline-detail { font-size: 11px; color: var(--accent); font-family: 'Geist Mono', monospace; }
        .lg-pipeline-detail--failed { color: var(--danger); }
        .lg-pipeline-row { display: flex; align-items: center; gap: 6px; }
        .lg-pipeline-step { display: flex; flex-direction: column; align-items: center; gap: 4px; flex: 1; min-width: 0; }
        .lg-pipeline-node { width: 22px; height: 22px; border-radius: 50%; display: flex; align-items: center; justify-content: center; border: 1.5px solid var(--border); background: var(--surface); color: var(--text-3); font-size: 10px; }
        .lg-pipeline-node--done { background: rgba(76, 175, 80, 0.15); border-color: #4CAF50; color: #4CAF50; }
        .lg-pipeline-node--current { background: rgba(66, 165, 245, 0.15); border-color: var(--accent); color: var(--accent); }
        .lg-pipeline-node--failed { background: rgba(239, 68, 68, 0.15); border-color: var(--danger); color: var(--danger); }
        .lg-pipeline-label { font-size: 9px; color: var(--text-3); font-family: 'Geist Mono', monospace; }
        .lg-pipeline-label--done { color: #4CAF50; }
        .lg-pipeline-label--current { color: var(--accent); font-weight: 600; }
        .lg-pipeline-label--failed { color: var(--danger); }
        .lg-pipeline-line { flex: 1; height: 2px; background: var(--border); min-width: 8px; }
        .lg-pipeline-line--done { background: #4CAF50; }
      `}</style>

      <div className="lg-wrap">
        <div className="lg-header">
          <IconTerminal2 size={22} stroke={2} />
          <div className="lg-title">Логи</div>
        </div>

        <div className="lg-card">
          <div className="lg-pipeline">
              <div className="lg-pipeline-head">
                <span className="lg-pipeline-title">Схема подключения</span>
                <span className={`lg-pipeline-detail${pipeline.failed ? ' lg-pipeline-detail--failed' : ''}`}>
                  {pipeline.visible ? pipelineStore.currentDetail(pipeline) : 'Ожидание…'}
                </span>
              </div>
              <div className="lg-pipeline-row">
                {(() => {
                  const steps = pipelineStore.getStepsToShow(pipeline);
                  return steps.map((step, idx) => {
                const isDone = pipeline.completed.includes(step) || pipeline.current === 'done';
                const isCurrent = pipeline.visible && pipeline.current === step;
                const isFailed = pipeline.failed === step;
                    const showLine = idx < steps.length - 1;
                    const nextDone = steps[idx + 1] ? (pipeline.completed.includes(steps[idx + 1]) || pipeline.current === 'done' || (pipeline.current && pipelineStore.stepOrder(pipeline.current) > pipelineStore.stepOrder(steps[idx + 1]))) : false;
                    return (
                      <React.Fragment key={step}>
                        <div className="lg-pipeline-step">
                          <div className={`lg-pipeline-node${isFailed ? ' lg-pipeline-node--failed' : isDone ? ' lg-pipeline-node--done' : isCurrent ? ' lg-pipeline-node--current' : ''}`}>
                            {isDone ? <IconCheck size={12} stroke={3} /> : isFailed ? <IconX size={12} stroke={3} /> : null}
                          </div>
                          <span className={`lg-pipeline-label${isFailed ? ' lg-pipeline-label--failed' : isDone ? ' lg-pipeline-label--done' : isCurrent ? ' lg-pipeline-label--current' : ''}`}>
                            {pipelineStore.stepLabel(step)}
                          </span>
                        </div>
                        {showLine && <div className={`lg-pipeline-line${isDone && nextDone ? ' lg-pipeline-line--done' : ''}`} />}
                      </React.Fragment>
                    );
                  });
                })()}
              </div>
            </div>

          <div className="lg-toolbar">
            <div className="lg-search-wrap">
              <input
                className="lg-search-input"
                placeholder="Поиск..."
                value={search}
                onChange={e => setSearch(e.target.value)}
              />
              <IconSearch size={17} className="lg-search-icon" />
            </div>
            <div className="lg-toolbar-right">
              <div className="lg-seg">
                {(['ALL', 'MINI', 'INFO', 'ERROR'] as Filter[]).map(f => (
                  <button
                    key={f}
                    className={`lg-seg-btn${filter === f ? ' lg-seg-btn--active' : ''}`}
                    onClick={() => setFilter(f)}
                  >{f}</button>
                ))}
              </div>
              <button className="lg-icon-btn" onClick={logStore.clear} title="Очистить" aria-label="Очистить логи">
                <IconTrashX stroke={2} size={16} />
              </button>
              <button className="lg-icon-btn" onClick={handleCopy} title="Копировать" aria-label="Копировать логи">
                <IconCopy stroke={2} size={16} />
              </button>
            </div>
          </div>

          {visible.length === 0 ? (
            <div className="lg-empty">
              <IconTerminal2 size={40} stroke={1.4} />
              {entries.length === 0 ? 'Логи появятся здесь...' : 'Ничего не найдено'}
            </div>
          ) : filter === 'MINI' ? (
            <div className="lg-list">
              {visible.map(e => (
                <div key={e.id} className="lg-mini-row">
                  <span className="lg-mini-arrow">{'>'}</span>
                  <span className="lg-mini-msg" style={{ color: MINI_COLORS[e.key || ''] || 'var(--text)' }}>{e.message}</span>
                  {e.count > 1 && <span className="lg-mini-badge">{'\u00d7'}{e.count}</span>}
                </div>
              ))}
            </div>
          ) : (
            <div className="lg-list" ref={listRef} onScroll={onScroll}>
              {visible.map(e => (
                <div key={e.id} className="lg-row">
                  <span className="lg-time">{e.time}</span>
                  <span className="lg-level" style={{ color: LEVEL_COLOR[e.level] }}>{e.level}</span>
                  <span className="lg-msg">{e.message}</span>
                </div>
              ))}
              <div ref={bottomRef} />
            </div>
          )}
        </div>
      </div>
    </>
  );
}
