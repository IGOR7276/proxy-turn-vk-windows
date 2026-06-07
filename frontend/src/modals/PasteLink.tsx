import { useState } from 'react';
import { IconLink, IconCheck, IconX } from '@tabler/icons-react';
import { parseWdttUrl } from '../lib/utils/wdttLink';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  onClose: () => void;
  onApply: (link: { ip: string; dtlsPort: string; password: string; hashes: string[]; name: string }) => void;
}

const EXAMPLE = 'wdtt://193.0.2.10:56000:56001:0:mypassword:hash1,hash2#MyServer';

export default function PasteLink({ onClose, onApply }: Props) {
  const [text, setText] = useState('');
  const [parsed, setParsed] = useState<ReturnType<typeof parseWdttUrl>>(null);
  const [error, setError] = useState<string | null>(null);

  const tryParse = (raw: string) => {
    setText(raw);
    setError(null);
    if (!raw.trim()) {
      setParsed(null);
      return;
    }
    const p = parseWdttUrl(raw.trim());
    if (!p) {
      setParsed(null);
      setError('Неверный формат ссылки');
      return;
    }
    setParsed(p);
  };

  const handleApply = () => {
    if (!parsed) {
      toastStore.show('Сначала вставьте корректную ссылку', 2000);
      return;
    }
    onApply(parsed);
    onClose();
  };

  return (
    <>
      <style>{`
        .pl-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .pl-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px 22px; width: 460px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .pl-header { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
        .pl-title { font-size: 16px; font-weight: 600; color: var(--text); flex: 1; }
        .pl-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .pl-sub { font-size: 12px; color: var(--text-3); margin-bottom: 14px; }
        .pl-format { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-input); padding: 10px 12px; margin-bottom: 14px; }
        .pl-format-label { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.4px; font-weight: 600; margin-bottom: 4px; }
        .pl-format-code { font-family: 'Geist Mono', monospace; font-size: 12px; color: var(--text-2); word-break: break-all; }
        .pl-format-legend { font-size: 11px; color: var(--text-3); margin-top: 8px; line-height: 1.5; }
        .pl-format-legend code { background: var(--bg-3); padding: 1px 5px; border-radius: 4px; color: var(--text-2); font-family: 'Geist Mono', monospace; font-size: 10px; }
        .pl-input { width: 100%; padding: 10px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 13px; font-family: 'Geist Mono', monospace; outline: none; box-sizing: border-box; resize: vertical; min-height: 60px; }
        .pl-input:focus { border-color: var(--accent); }
        .pl-input--error { border-color: var(--danger); }
        .pl-input--ok { border-color: var(--success); }
        .pl-status { display: flex; align-items: center; gap: 6px; margin-top: 8px; font-size: 12px; }
        .pl-status--err { color: var(--danger); }
        .pl-status--ok { color: var(--success); }
        .pl-preview { background: var(--accent-soft); border: 1px solid var(--accent); border-radius: var(--r-input); padding: 10px 12px; margin-top: 10px; font-size: 12px; }
        .pl-preview-row { display: flex; gap: 8px; padding: 2px 0; color: var(--text); }
        .pl-preview-key { color: var(--text-3); min-width: 80px; }
        .pl-preview-val { font-family: 'Geist Mono', monospace; color: var(--text); word-break: break-all; flex: 1; }
        .pl-actions { display: flex; gap: 8px; margin-top: 14px; }
        .pl-btn { flex: 1; padding: 12px; border: none; border-radius: var(--r-input); font-family: 'Geist', sans-serif; font-size: 14px; font-weight: 600; cursor: pointer; }
        .pl-btn:disabled { opacity: 0.4; cursor: not-allowed; }
        .pl-btn--cancel { background: var(--bg-2); color: var(--text-2); border: 1px solid var(--border); }
        .pl-btn--ok { background: var(--accent); color: var(--accent-fg); }
      `}</style>
      <div className="pl-overlay" onClick={onClose}>
        <div className="pl-modal" onClick={e => e.stopPropagation()}>
          <div className="pl-header">
            <IconLink stroke={2} size={20} />
            <span className="pl-title">Вставить wdtt:// ссылку</span>
            <button className="pl-close" onClick={onClose}>✕</button>
          </div>
          <div className="pl-sub">Вставьте ссылку от администратора сервера — профиль создастся автоматически.</div>

          <div className="pl-format">
            <div className="pl-format-label">Формат</div>
            <div className="pl-format-code">{EXAMPLE}</div>
            <div className="pl-format-legend">
              <code>wdtt://IP:DTLS:WG:PROXY:PASSWORD[:HASH1,HASH2,#имя]</code><br />
              <code>#имя</code> — опциональное имя профиля после решётки.<br />
              <code>hash1,hash2</code> — VK-хеши через запятую (опционально).
            </div>
          </div>

          <textarea
            className={`pl-input${error ? ' pl-input--error' : parsed ? ' pl-input--ok' : ''}`}
            placeholder="wdtt://..."
            value={text}
            onChange={e => tryParse(e.target.value)}
            onPaste={e => {
              const pasted = e.clipboardData.getData('text');
              if (pasted) {
                setTimeout(() => tryParse(pasted), 0);
              }
            }}
            autoFocus
            spellCheck={false}
          />

          {error && <div className="pl-status pl-status--err"><IconX size={14} /> {error}</div>}
          {parsed && !error && (
            <div className="pl-status pl-status--ok"><IconCheck size={14} /> Ссылка распознана</div>
          )}

          {parsed && (
            <div className="pl-preview">
              <div className="pl-preview-row"><span className="pl-preview-key">Имя:</span><span className="pl-preview-val">{parsed.name}</span></div>
              <div className="pl-preview-row"><span className="pl-preview-key">IP:</span><span className="pl-preview-val">{parsed.ip}:{parsed.dtlsPort}</span></div>
              <div className="pl-preview-row"><span className="pl-preview-key">Пароль:</span><span className="pl-preview-val">{parsed.password}</span></div>
              {parsed.hashes.length > 0 && <div className="pl-preview-row"><span className="pl-preview-key">Хешей:</span><span className="pl-preview-val">{parsed.hashes.length}</span></div>}
            </div>
          )}

          <div className="pl-actions">
            <button className="pl-btn pl-btn--cancel" onClick={onClose}>Отмена</button>
            <button className="pl-btn pl-btn--ok" onClick={handleApply} disabled={!parsed}>Применить</button>
          </div>
        </div>
      </div>
    </>
  );
}

