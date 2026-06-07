import { useState } from 'react';
import { IconHash, IconTrash, IconX } from '@tabler/icons-react';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  hashes: [string, string, string, string];
  onClose: () => void;
  onSave: (hashes: [string, string, string, string]) => void;
}

function extractHash(raw: string): string {
  const v = raw.trim();
  if (!v) return '';
  const lower = v.toLowerCase();
  const marker = '/call/join/';
  const idx = lower.indexOf(marker);
  if (idx !== -1) {
    return v.slice(idx + marker.length).split(/[?#\s]/)[0].trim();
  }
  return v.split(/[?#\s]/)[0].trim();
}

export default function Hash({ hashes, onClose, onSave }: Props) {
  const [values, setValues] = useState<[string, string, string, string]>(
    hashes.map(h => extractHash(h)) as [string, string, string, string]
  );

  const set = (i: number, v: string) => {
    const next = [...values] as [string, string, string, string];
    next[i] = v;
    setValues(next);
  };

  const setAll = (next: [string, string, string, string]) => setValues(next);

  const clearOne = (i: number) => set(i, '');

  const clearAll = () => setAll(['', '', '', '']);

  const save = () => {
    const normalized = values.map(extractHash) as [string, string, string, string];
    const nonEmpty = normalized.filter(v => v !== '');
    if (new Set(nonEmpty).size !== nonEmpty.length) {
      toastStore.show('Обнаружены дублирующиеся хеши');
      return;
    }
    onSave(normalized);
    onClose();
  };

  const filledCount = values.filter(v => v.trim()).length;

  return (
    <>
      <style>{`
        .hs-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .hs-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px 22px; width: 440px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .hs-header { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
        .hs-title { font-size: 16px; font-weight: 600; color: var(--text); flex: 1; }
        .hs-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 4px; display: flex; }
        .hs-close:hover { color: var(--accent); }
        .hs-hint { font-size: 12px; color: var(--text-3); margin-bottom: 14px; }
        .hs-list { display: flex; flex-direction: column; gap: 8px; margin-bottom: 14px; }
        .hs-field { position: relative; display: flex; align-items: center; }
        .hs-num { position: absolute; left: 12px; font-size: 12px; color: var(--text-3); font-weight: 600; font-family: 'Geist Mono', monospace; pointer-events: none; }
        .hs-input { width: 100%; padding: 11px 36px 11px 36px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 13px; font-family: 'Geist Mono', monospace; outline: none; box-sizing: border-box; }
        .hs-input:focus { border-color: var(--accent); }
        .hs-input--filled { border-color: var(--accent); background: var(--accent-soft); }
        .hs-clear-one { position: absolute; right: 8px; background: none; border: none; cursor: pointer; color: var(--text-3); padding: 4px; display: flex; align-items: center; border-radius: 6px; }
        .hs-clear-one:hover { color: var(--danger); background: var(--surface-2); }
        .hs-footer { display: flex; align-items: center; gap: 8px; }
        .hs-info { flex: 1; font-size: 12px; color: var(--text-3); }
        .hs-info strong { color: var(--accent); font-weight: 700; }
        .hs-btn { padding: 10px 16px; border: none; border-radius: var(--r-input); font-family: 'Geist', sans-serif; font-size: 13px; font-weight: 600; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; }
        .hs-btn-ghost { background: var(--surface); color: var(--text-2); border: 1px solid var(--border); }
        .hs-btn-ghost:hover { color: var(--danger); border-color: var(--danger); }
        .hs-btn-primary { background: var(--accent); color: var(--accent-fg); padding: 10px 22px; }
      `}</style>
      <div className="hs-overlay" onClick={onClose}>
        <div className="hs-modal" onClick={e => e.stopPropagation()}>
          <div className="hs-header">
            <IconHash stroke={2} size={20} />
            <span className="hs-title">VK хеши</span>
            <button className="hs-close" onClick={onClose} title="Закрыть (Esc)">
              <IconX size={18} stroke={2} />
            </button>
          </div>
          <div className="hs-hint">
            Можно вставлять полную ссылку <code>vk.com/call/join/…</code> — хеш извлечётся автоматически.
          </div>

          <div className="hs-list">
            {[0, 1, 2, 3].map(i => {
              const filled = !!values[i]?.trim();
              return (
                <div key={i} className="hs-field">
                  <span className="hs-num">#{i + 1}</span>
                  <input
                    className={`hs-input${filled ? ' hs-input--filled' : ''}`}
                    placeholder={`ключ ${i + 1}`}
                    value={values[i]}
                    onChange={e => set(i, e.target.value)}
                    onKeyDown={e => {
                      if (e.key === 'Enter') save();
                      if (e.key === 'Escape') onClose();
                    }}
                    spellCheck={false}
                    autoCorrect="off"
                    autoCapitalize="off"
                  />
                  {filled && (
                    <button
                      className="hs-clear-one"
                      onClick={() => clearOne(i)}
                      title="Очистить поле"
                    >
                      <IconX size={14} stroke={2.5} />
                    </button>
                  )}
                </div>
              );
            })}
          </div>

          <div className="hs-footer">
            <div className="hs-info">Заполнено: <strong>{filledCount}</strong>/4</div>
            <button
              className="hs-btn hs-btn-ghost"
              onClick={clearAll}
              disabled={filledCount === 0}
              title="Очистить все 4 поля"
            >
              <IconTrash size={14} stroke={2} />
              Очистить
            </button>
            <button
              className="hs-btn hs-btn-primary"
              onClick={save}
            >
              Сохранить
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
