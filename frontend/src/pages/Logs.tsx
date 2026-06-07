import { useState, useEffect, useRef, useCallback } from 'react';
import { IconSearch, IconTrashX, IconCopy, IconTerminal2 } from '@tabler/icons-react';
import { logStore, type LogEntry, type LogLevel } from '../lib/stores/logStore';

type Filter = 'ALL' | 'INFO' | 'ERROR';

const LEVEL_COLOR: Record<LogLevel, string> = {
  INFO:  'var(--text)',
  WARN:  '#f59e0b',
  ERROR: '#ef4444',
  DEBUG: 'var(--text-3)',
};

export default function Logs() {
  const [filter, setFilter] = useState<Filter>('ALL');
  const [search, setSearch] = useState('');
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const autoScroll = useRef(true);

  useEffect(() => logStore.subscribe(setEntries), []);

  useEffect(() => {
    if (autoScroll.current) bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [entries]);

  const onScroll = useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }, []);

  const visible = entries.filter(e => {
    if (filter !== 'ALL' && e.level !== filter) return false;
    if (search && !e.message.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const handleCopy = () => {
    const text = visible.map(e => `[${e.time}] [${e.level}] ${e.message}${e.count > 1 ? ` (×${e.count})` : ''}`).join('\n');
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
        .lg-row { display: flex; align-items: baseline; gap: 10px; padding: 5px 18px; font-size: 13px; line-height: 1.5; }
        .lg-row:hover { background: var(--bg-2); }
        .lg-time { color: var(--text-4); flex-shrink: 0; font-size: 12px; font-variant-numeric: tabular-nums; }
        .lg-level { flex-shrink: 0; font-weight: 700; font-size: 12px; width: 48px; }
        .lg-msg { flex: 1; word-break: break-all; color: var(--text); }
        .lg-count { flex-shrink: 0; background: var(--seg-bg); border-radius: 20px; padding: 1px 8px; font-size: 11px; color: var(--text-2); }
        .lg-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: var(--text-4); font-size: 14px; }
        .lg-empty svg { color: var(--text-4); }
      `}</style>

      <div className="lg-wrap">
        <div className="lg-header">
          <IconTerminal2 size={22} stroke={2} />
          <div className="lg-title">Логи</div>
        </div>

        <div className="lg-card">
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
                {(['ALL', 'INFO', 'ERROR'] as Filter[]).map(f => (
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
          ) : (
            <div className="lg-list" ref={listRef} onScroll={onScroll}>
              {visible.map(e => (
                <div key={e.id} className="lg-row">
                  <span className="lg-time">{e.time}</span>
                  <span className="lg-level" style={{ color: LEVEL_COLOR[e.level] }}>{e.level}</span>
                  <span className="lg-msg">{e.message}</span>
                  {e.count > 1 && <span className="lg-count">×{e.count}</span>}
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

