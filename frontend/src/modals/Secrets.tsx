import { useState } from 'react';
import { IconKey, IconLink, IconLock, IconCopy, IconCheck } from '@tabler/icons-react';
import type { Server } from '../lib/types';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  server: Server;
  onClose: () => void;
}

export default function Secrets({ server, onClose }: Props) {
  const [copied, setCopied] = useState<string | null>(null);

  const copy = (key: string, value: string) => {
    navigator.clipboard.writeText(value);
    setCopied(key);
    setTimeout(() => setCopied(null), 1200);
    toastStore.show('Скопировано', 1200);
  };

  return (
    <>
      <style>{`
        .sc-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .sc-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px 22px; width: 400px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .sc-header { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; }
        .sc-title { font-size: 16px; font-weight: 600; color: var(--text); flex: 1; }
        .sc-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .sc-row { display: flex; align-items: center; gap: 10px; padding: 10px 12px; background: var(--surface-2); border-radius: var(--r-input); margin-bottom: 8px; }
        .sc-icon { color: var(--text-3); flex-shrink: 0; }
        .sc-label { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.4px; font-weight: 600; min-width: 60px; }
        .sc-val { flex: 1; font-family: 'Geist Mono', monospace; font-size: 13px; color: var(--text); word-break: break-all; }
        .sc-val--empty { color: var(--text-4); font-style: italic; }
        .sc-copy { width: 32px; height: 32px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); cursor: pointer; display: flex; align-items: center; justify-content: center; color: var(--text-3); transition: background 0.12s, color 0.12s; padding: 0; flex-shrink: 0; }
        .sc-copy:hover { background: var(--bg-2); color: var(--text); }
        .sc-copy--ok { color: var(--success); }
        .sc-footer { margin-top: 14px; text-align: center; font-size: 11px; color: var(--text-3); }
      `}</style>
      <div className="sc-overlay" onClick={onClose}>
        <div className="sc-modal" onClick={e => e.stopPropagation()}>
          <div className="sc-header">
            <IconKey stroke={2} size={20} />
            <span className="sc-title">Секреты — {server.name}</span>
            <button className="sc-close" onClick={onClose}>✕</button>
          </div>

          <div className="sc-row">
            <IconLink size={16} className="sc-icon" />
            <span className="sc-label">Peer</span>
            <span className="sc-val">{server.host}</span>
            <button
              className={`sc-copy ${copied === 'host' ? 'sc-copy--ok' : ''}`}
              onClick={() => copy('host', server.host)}
              title="Копировать"
            >
              {copied === 'host' ? <IconCheck size={14} /> : <IconCopy size={14} stroke={2} />}
            </button>
          </div>

          <div className="sc-row">
            <IconLock size={16} className="sc-icon" />
            <span className="sc-label">Пароль</span>
            <span className={`sc-val ${!server.password ? 'sc-val--empty' : ''}`}>
              {server.password || 'не задан'}
            </span>
            {server.password && (
              <button
                className={`sc-copy ${copied === 'pwd' ? 'sc-copy--ok' : ''}`}
                onClick={() => copy('pwd', server.password)}
                title="Копировать"
              >
                {copied === 'pwd' ? <IconCheck size={14} /> : <IconCopy size={14} stroke={2} />}
              </button>
            )}
          </div>

          <div className="sc-footer">все данные отображаются открытым текстом</div>
        </div>
      </div>
    </>
  );
}
