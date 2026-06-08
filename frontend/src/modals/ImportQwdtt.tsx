import { useState, useRef } from 'react';
import { IconFileImport, IconCheck, IconX, IconUpload } from '@tabler/icons-react';
import { parseQwdtt, type QwdttImportResult } from '../lib/utils/qwdttParser';

interface Props {
  onClose: () => void;
  onImport: (result: QwdttImportResult) => void;
}

export default function ImportQwdtt({ onClose, onImport }: Props) {
  const [text, setText] = useState('');
  const [parsed, setParsed] = useState<QwdttImportResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const tryParse = (raw: string) => {
    setText(raw);
    setError(null);
    if (!raw.trim()) {
      setParsed(null);
      return;
    }
    const p = parseQwdtt(raw.trim());
    if (!p || p.profiles.length === 0) {
      setParsed(null);
      setError('Не удалось распознать формат .qwdtt');
      return;
    }
    setParsed(p);
  };

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      const content = reader.result as string;
      tryParse(content);
    };
    reader.readAsText(file);
    e.target.value = '';
  };

  const handleImport = () => {
    if (!parsed) return;
    onImport(parsed);
    onClose();
  };

  return (
    <>
      <style>{`
        .iq-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .iq-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px 22px; width: 500px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; max-height: 90vh; overflow-y: auto; }
        .iq-header { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
        .iq-title { font-size: 16px; font-weight: 600; color: var(--text); flex: 1; }
        .iq-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .iq-sub { font-size: 12px; color: var(--text-3); margin-bottom: 14px; }
        .iq-file-btn { display: flex; align-items: center; gap: 8px; padding: 10px 16px; border: 1.5px dashed var(--border); border-radius: var(--r-input); background: var(--bg-2); color: var(--text); font-size: 13px; font-family: 'Geist', sans-serif; cursor: pointer; width: 100%; justify-content: center; transition: border-color 0.12s, background 0.12s; margin-bottom: 12px; }
        .iq-file-btn:hover { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); }
        .iq-divider { display: flex; align-items: center; gap: 8px; margin: 4px 0 12px; color: var(--text-4); font-size: 12px; }
        .iq-divider::before, .iq-divider::after { content: ''; flex: 1; height: 1px; background: var(--border); }
        .iq-input { width: 100%; padding: 10px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 13px; font-family: 'Geist Mono', monospace; outline: none; box-sizing: border-box; resize: vertical; min-height: 80px; }
        .iq-input:focus { border-color: var(--accent); }
        .iq-input--error { border-color: var(--danger); }
        .iq-input--ok { border-color: var(--success); }
        .iq-status { display: flex; align-items: center; gap: 6px; margin-top: 8px; font-size: 12px; }
        .iq-status--err { color: var(--danger); }
        .iq-status--ok { color: var(--success); }
        .iq-preview { background: var(--accent-soft); border: 1px solid var(--accent); border-radius: var(--r-input); padding: 10px 12px; margin-top: 10px; }
        .iq-preview-title { font-size: 12px; font-weight: 600; color: var(--accent); margin-bottom: 6px; }
        .iq-preview-row { display: flex; gap: 8px; padding: 3px 0; font-size: 12px; color: var(--text); }
        .iq-preview-key { color: var(--text-3); min-width: 90px; flex-shrink: 0; }
        .iq-preview-val { font-family: 'Geist Mono', monospace; color: var(--text); word-break: break-all; flex: 1; }
        .iq-preview-hashes { font-family: 'Geist Mono', monospace; font-size: 11px; color: var(--text-2); word-break: break-all; }
        .iq-actions { display: flex; gap: 8px; margin-top: 14px; }
        .iq-btn { flex: 1; padding: 12px; border: none; border-radius: var(--r-input); font-family: 'Geist', sans-serif; font-size: 14px; font-weight: 600; cursor: pointer; }
        .iq-btn:disabled { opacity: 0.4; cursor: not-allowed; }
        .iq-btn--cancel { background: var(--bg-2); color: var(--text-2); border: 1px solid var(--border); }
        .iq-btn--ok { background: var(--accent); color: var(--accent-fg); }
      `}</style>
      <div className="iq-overlay" onClick={onClose}>
        <div className="iq-modal" onClick={e => e.stopPropagation()}>
          <div className="iq-header">
            <IconFileImport stroke={2} size={20} />
            <span className="iq-title">Импорт .qwdtt</span>
            <button className="iq-close" onClick={onClose}>✕</button>
          </div>
          <div className="iq-sub">Выберите файл .qwdtt или вставьте содержимое вручную.</div>

          <button className="iq-file-btn" onClick={() => fileRef.current?.click()}>
            <IconUpload size={16} />
            Выбрать файл .qwdtt
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".qwdtt,.conf,.json,text/plain"
            style={{ display: 'none' }}
            onChange={handleFile}
          />

          <div className="iq-divider">или вставьте текст</div>

          <textarea
            className={`iq-input${error ? ' iq-input--error' : parsed ? ' iq-input--ok' : ''}`}
            placeholder="Вставьте JSON или qwdtt:// ссылку..."
            value={text}
            onChange={e => tryParse(e.target.value)}
            spellCheck={false}
          />

          {error && <div className="iq-status iq-status--err"><IconX size={14} /> {error}</div>}
          {parsed && !error && (
            <div className="iq-status iq-status--ok">
              <IconCheck size={14} /> Распознано профилей: {parsed.profiles.length}
              {parsed.groupName && <> (группа: {parsed.groupName})</>}
            </div>
          )}

          {parsed && (
            <div className="iq-preview">
              <div className="iq-preview-title">{parsed.groupName || 'Профили'}</div>
              {parsed.profiles.slice(0, 5).map((p, i) => (
                <div key={i} className="iq-preview-row">
                  <span className="iq-preview-key">{p.name}:</span>
                  <span className="iq-preview-val">
                    {p.peer} · {p.workers}w
                    {p.hashes.length > 0 && (
                      <> · <span className="iq-preview-hashes">{p.hashes.length} хешей</span></>
                    )}
                  </span>
                </div>
              ))}
              {parsed.profiles.length > 5 && (
                <div className="iq-preview-row">
                  <span className="iq-preview-key" />
                  <span className="iq-preview-val" style={{ color: 'var(--text-3)' }}>
                    ...и ещё {parsed.profiles.length - 5}
                  </span>
                </div>
              )}
            </div>
          )}

          <div className="iq-actions">
            <button className="iq-btn iq-btn--cancel" onClick={onClose}>Отмена</button>
            <button className="iq-btn iq-btn--ok" onClick={handleImport} disabled={!parsed}>
              Импортировать ({parsed?.profiles.length ?? 0})
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
