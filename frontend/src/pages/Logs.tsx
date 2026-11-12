import React, { useState, useEffect, useRef, useCallback, useMemo, useLayoutEffect } from 'react';
import {
  IconSearch, IconTrashX, IconCopy, IconTerminal2, IconCheck, IconX, IconDownload,
} from '@tabler/icons-react';
import { logStore, type LogEntry, type LogLevel } from '../lib/stores/logStore';
import { pipelineStore, type PipelineState } from '../lib/stores/pipelineStore';
import { toastStore } from '../lib/stores/toastStore';
import { ExportLogs } from '../../wailsjs/go/backend/App';
import IconButton from '../components/ui/IconButton';
import styles from './Logs.module.css';

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
  if (kv === '__workers__' || kv === '__wg__' || kv === 'wrap' || kv === 'dtls' ||
      kv === 'ready' || kv === 'creds' || kv === 'creds_ok' || kv === 'turn' || kv === 'state') {
    return { ...entry, message: msg.replace(/^\[[^\]]+\]\s*/, '') };
  }
  return entry;
}

// Виртуализация: фиксированная высота строки, рендерим только окно.
const ROW_H = 30;
const OVERSCAN = 10;

export default function Logs() {
  const [filter, setFilter] = useState<Filter>('MINI');
  const [search, setSearch] = useState('');
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [pipeline, setPipeline] = useState<PipelineState>({
    visible: false, current: null, completed: [], failed: null, timedOut: false, timeoutSec: 0,
  });
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportH, setViewportH] = useState(600);
  const listRef = useRef<HTMLDivElement>(null);
  const autoScroll = useRef(true);

  useEffect(() => logStore.subscribe(setEntries), []);
  useEffect(() => pipelineStore.subscribe(setPipeline), []);

  useLayoutEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const measure = () => setViewportH(el.clientHeight || 600);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [filter]);

  useEffect(() => {
    const el = listRef.current;
    if (el) {
      el.scrollTop = 0;
      setScrollTop(0);
    }
    autoScroll.current = true;
  }, [filter, search]);

  useEffect(() => {
    const el = listRef.current;
    if (!el || filter === 'MINI' || !autoScroll.current) return;
    el.scrollTop = el.scrollHeight;
  }, [entries.length, filter]);

  const onScroll = useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    setScrollTop(el.scrollTop);
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }, []);

  const miniEntries = useMemo(() => {
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
  }, [entries]);

  const visible = useMemo(() => {
    const base = filter === 'MINI' ? miniEntries : entries;
    const q = search.trim().toLowerCase();
    return base.filter(e => {
      if (filter !== 'ALL' && filter !== 'MINI' && e.level !== filter) return false;
      if (q && !e.message.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [entries, miniEntries, filter, search]);

  const total = visible.length;
  const start = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN);
  const end = Math.min(total, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN);
  const slice = visible.slice(start, end);

  const handleCopy = async () => {
    const text = visible.map(e => `[${e.time}] [${e.level}] ${e.message}`).join('\n');
    try {
      await navigator.clipboard.writeText(text);
      toastStore.show(`Скопировано строк: ${visible.length}`, 1800);
    } catch {
      toastStore.show('Не удалось скопировать логи', 2500);
    }
  };

  const handleExport = async () => {
    try {
      const path = await ExportLogs();
      if (path) toastStore.show(`Логи сохранены: ${path}`, 4000);
      else toastStore.show('Экспорт отменён', 1800);
    } catch (err) {
      toastStore.show(`Ошибка экспорта: ${String(err)}`, 4000);
    }
  };

  const rowClass = filter === 'MINI' ? styles.miniRow : styles.row;

  return (
    <div className={styles.wrap}>
      <div className={styles.header}>
        <IconTerminal2 size={22} stroke={2} aria-hidden="true" />
        <h1 className={styles.title}>Логи</h1>
      </div>

      <div className={styles.card}>
        <div className={styles.pipeline}>
          <div className={styles.pipelineHead}>
            <span className={styles.pipelineTitle}>Схема подключения</span>
            <span className={`${styles.pipelineDetail}${pipeline.failed ? ` ${styles.pipelineDetailFailed}` : ''}`}>
              {pipeline.visible ? pipelineStore.currentDetail(pipeline) : 'Ожидание…'}
            </span>
          </div>
          <div className={styles.pipelineRow}>
            {(() => {
              const steps = pipelineStore.getStepsToShow(pipeline);
              return steps.map((step, idx) => {
                const isDone = pipeline.completed.includes(step) || pipeline.current === 'done';
                const isCurrent = pipeline.visible && pipeline.current === step;
                const isFailed = pipeline.failed === step;
                const showLine = idx < steps.length - 1;
                const nextDone = steps[idx + 1]
                  ? (pipeline.completed.includes(steps[idx + 1]) || pipeline.current === 'done' ||
                     (pipeline.current && pipelineStore.stepOrder(pipeline.current) > pipelineStore.stepOrder(steps[idx + 1])))
                  : false;
                return (
                  <React.Fragment key={step}>
                    <div className={styles.pipelineStep}>
                      <div className={`${styles.pipelineNode}${isFailed ? ` ${styles.pipelineNodeFailed}` : isDone ? ` ${styles.pipelineNodeDone}` : isCurrent ? ` ${styles.pipelineNodeCurrent}` : ''}`}>
                        {isDone ? <IconCheck size={12} stroke={3} /> : isFailed ? <IconX size={12} stroke={3} /> : null}
                      </div>
                      <span className={`${styles.pipelineLabel}${isFailed ? ` ${styles.pipelineLabelFailed}` : isDone ? ` ${styles.pipelineLabelDone}` : isCurrent ? ` ${styles.pipelineLabelCurrent}` : ''}`}>
                        {pipelineStore.stepLabel(step)}
                      </span>
                    </div>
                    {showLine && <div className={`${styles.pipelineLine}${isDone && nextDone ? ` ${styles.pipelineLineDone}` : ''}`} />}
                  </React.Fragment>
                );
              });
            })()}
          </div>
        </div>

        <div className={styles.toolbar}>
          <div className={styles.searchWrap}>
            <input
              className={styles.searchInput}
              placeholder="Поиск..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              aria-label="Поиск по логам"
              type="search"
            />
            <IconSearch size={17} className={styles.searchIcon} aria-hidden="true" />
          </div>
          <div className={styles.toolbarRight}>
            <div className={styles.seg} role="group" aria-label="Фильтр логов">
              {(['ALL', 'MINI', 'INFO', 'ERROR'] as Filter[]).map(f => (
                <button
                  key={f}
                  type="button"
                  className={`${styles.segBtn}${filter === f ? ` ${styles.segBtnActive}` : ''}`}
                  onClick={() => setFilter(f)}
                  aria-pressed={filter === f}
                >{f}</button>
              ))}
            </div>
            <IconButton label="Экспортировать логи в файл" onClick={handleExport}>
              <IconDownload stroke={2} size={16} />
            </IconButton>
            <IconButton label="Очистить логи" onClick={logStore.clear}>
              <IconTrashX stroke={2} size={16} />
            </IconButton>
            <IconButton label="Копировать логи" onClick={handleCopy}>
              <IconCopy stroke={2} size={16} />
            </IconButton>
          </div>
        </div>

        {total === 0 ? (
          <div className={styles.empty}>
            <IconTerminal2 size={40} stroke={1.4} aria-hidden="true" />
            {entries.length === 0 ? 'Логи появятся здесь...' : 'Ничего не найдено'}
          </div>
        ) : (
          <div
            className={styles.list}
            ref={listRef}
            onScroll={onScroll}
            role="log"
            aria-live="polite"
            aria-label="Журнал логов"
            tabIndex={0}
          >
            <div className={styles.spacer} style={{ height: total * ROW_H }}>
              <div className={styles.viewport} style={{ transform: `translateY(${start * ROW_H}px)` }}>
                {slice.map(e => (
                  filter === 'MINI' ? (
                    <div key={e.id} className={rowClass} style={{ height: ROW_H }}>
                      <span className={styles.miniArrow} aria-hidden="true">{'>'}</span>
                      <span className={styles.miniMsg} style={{ color: MINI_COLORS[e.key || ''] || 'var(--text)' }} title={e.message}>
                        {e.message}
                      </span>
                      {e.count > 1 && <span className={styles.miniBadge}>{'\u00d7'}{e.count}</span>}
                    </div>
                  ) : (
                    <div key={e.id} className={rowClass} style={{ height: ROW_H }}>
                      <span className={styles.time}>{e.time}</span>
                      <span className={styles.level} style={{ color: LEVEL_COLOR[e.level] }}>{e.level}</span>
                      <span className={styles.msg} title={e.message}>{e.message}</span>
                    </div>
                  )
                ))}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
