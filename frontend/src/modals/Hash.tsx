import { useState } from 'react';
import { IconHash, IconX } from '@tabler/icons-react';

interface Props {
  hashes: [string, string, string, string];
  onClose: () => void;
  onSave: (hashes: [string, string, string, string]) => void;
}

export default function Hash({ hashes, onClose, onSave }: Props) {
  const [values, setValues] = useState<[string, string, string, string]>([...hashes] as [string, string, string, string]);
  const [editingIdx, setEditingIdx] = useState<number | null>(null);
  const [editValue, setEditValue] = useState('');

  const set = (i: number, v: string) => {
    const next = [...values] as [string, string, string, string];
    next[i] = v;
    setValues(next);
  };

  const startEdit = (i: number) => {
    setEditingIdx(i);
    setEditValue(values[i] || '');
  };

  const commitEdit = () => {
    if (editingIdx === null) return;
    set(editingIdx, editValue.trim());
    setEditingIdx(null);
    setEditValue('');
  };

  const clearOne = (i: number) => {
    set(i, '');
    if (editingIdx === i) setEditingIdx(null);
  };

  const filledCount = values.filter(v => v.trim()).length;

  return (
    <>
      <style>{`
        .hs-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .hs-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px 22px; width: 400px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .hs-header { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; }
        .hs-title { font-size: 16px; font-weight: 600; color: var(--text); flex: 1; }
        .hs-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .hs-list { display: flex; flex-direction: column; gap: 8px; margin-bottom: 16px; }
        .hs-row { display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--r-input); cursor: pointer; transition: border-color 0.15s, background 0.15s; }
        .hs-row:hover { border-color: var(--text-3); }
        .hs-row--filled { background: var(--accent-soft); border-color: var(--accent); }
        .hs-row--filled .hs-row-status { color: var(--accent); }
        .hs-row-num { font-size: 13px; color: var(--text-3); font-weight: 600; font-family: 'Geist Mono', monospace; min-width: 18px; }
        .hs-row-status { flex: 1; font-size: 13px; color: var(--text-3); font-family: 'Geist Mono', monospace; }
        .hs-row--filled .hs-row-status { color: var(--accent); font-weight: 600; }
        .hs-row-clear { background: none; border: none; cursor: pointer; color: var(--text-3); padding: 0; display: flex; align-items: center; opacity: 0; transition: opacity 0.15s, color 0.15s; }
        .hs-row:hover .hs-row-clear { opacity: 1; }
        .hs-row-clear:hover { color: var(--danger); }
        .hs-editor { display: flex; flex-direction: column; gap: 8px; padding: 12px; background: var(--bg-2); border-radius: var(--r-input); margin-bottom: 12px; }
        .hs-editor-input { padding: 10px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 13px; font-family: 'Geist Mono', monospace; outline: none; width: 100%; box-sizing: border-box; }
        .hs-editor-input:focus { border-color: var(--accent); }
        .hs-editor-actions { display: flex; gap: 8px; }
        .hs-editor-actions button { flex: 1; padding: 8px; border: none; border-radius: var(--r-input); font-family: 'Geist', sans-serif; font-size: 13px; font-weight: 600; cursor: pointer; }
        .hs-editor-actions .hs-cancel { background: var(--surface); color: var(--text-2); border: 1px solid var(--border); }
        .hs-editor-actions .hs-save { background: var(--accent); color: var(--accent-fg); }
        .hs-footer { display: flex; gap: 8px; align-items: center; }
        .hs-footer-info { flex: 1; font-size: 12px; color: var(--text-3); }
        .hs-footer-info strong { color: var(--accent); font-weight: 700; }
        .hs-save-btn { padding: 10px 20px; border: none; border-radius: var(--r-input); background: var(--accent); color: var(--accent-fg); font-family: 'Geist', sans-serif; font-size: 13px; font-weight: 600; cursor: pointer; }
      `}</style>
      <div className="hs-overlay" onClick={onClose}>
        <div className="hs-modal" onClick={e => e.stopPropagation()}>
          <div className="hs-header">
            <IconHash stroke={2} size={20} />
            <span className="hs-title">VK хеши</span>
            <button className="hs-close" onClick={onClose}>✕</button>
          </div>

          {editingIdx !== null && (
            <div className="hs-editor">
              <input
                className="hs-editor-input"
                placeholder={`Hash #${editingIdx + 1}`}
                value={editValue}
                onChange={e => setEditValue(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') commitEdit();
                  if (e.key === 'Escape') { setEditingIdx(null); setEditValue(''); }
                }}
                autoFocus
              />
              <div className="hs-editor-actions">
                <button className="hs-cancel" onClick={() => { setEditingIdx(null); setEditValue(''); }}>Отмена</button>
                <button className="hs-save" onClick={commitEdit}>Сохранить</button>
              </div>
            </div>
          )}

          <div className="hs-list">
            {[0, 1, 2, 3].map(i => {
              const filled = !!values[i]?.trim();
              return (
                <div
                  key={i}
                  className={`hs-row${filled ? ' hs-row--filled' : ''}`}
                  onClick={() => startEdit(i)}
                >
                  <span className="hs-row-num">#{i + 1}</span>
                  <span className="hs-row-status">{filled ? 'заполнен ✓' : 'пусто — нажмите'}</span>
                  {filled && (
                    <button
                      className="hs-row-clear"
                      onClick={e => { e.stopPropagation(); clearOne(i); }}
                      title="Очистить"
                    >
                      <IconX size={14} stroke={2.5} />
                    </button>
                  )}
                </div>
              );
            })}
          </div>

          <div className="hs-footer">
            <div className="hs-footer-info">Заполнено: <strong>{filledCount}</strong>/4</div>
            <button
              className="hs-save-btn"
              onClick={() => { onSave(values); onClose(); }}
            >
              Готово
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
