import { useState } from 'react';
import { IconX, IconMinus, IconPower, IconQuestionMark } from '@tabler/icons-react';

interface Props {
  onClose: () => void;
  onChoose: (action: 'hide' | 'exit', remember: boolean) => void;
}

export default function CloseDialog({ onClose, onChoose }: Props) {
  const [remember, setRemember] = useState(false);

  const choose = (action: 'hide' | 'exit') => {
    onChoose(action, remember);
  };

  return (
    <>
      <style>{`
        .cd-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 300; animation: overlay-in 0.2s ease-out; }
        .cd-modal { background: var(--surface); border-radius: var(--r-card); padding: 22px 24px; width: 380px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.25s ease-out; }
        .cd-head { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
        .cd-icon { width: 36px; height: 36px; border-radius: 10px; background: var(--accent-soft); color: var(--accent); display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
        .cd-title { font-size: 16px; font-weight: 600; color: var(--text); flex: 1; line-height: 1.3; }
        .cd-msg { font-size: 13px; color: var(--text-2); line-height: 1.5; margin-bottom: 18px; padding-left: 46px; }
        .cd-remember { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--r-input); background: var(--surface-2); margin-bottom: 16px; cursor: pointer; user-select: none; transition: border-color 0.12s, background 0.12s; }
        .cd-remember:hover { border-color: var(--text-3); }
        .cd-remember input { margin: 0; width: 16px; height: 16px; accent-color: var(--accent); cursor: pointer; }
        .cd-remember-label { font-size: 13px; color: var(--text); font-weight: 500; flex: 1; }
        .cd-remember-hint { font-size: 11px; color: var(--text-3); margin-top: 2px; font-weight: 400; }
        .cd-actions { display: flex; gap: 8px; }
        .cd-btn { flex: 1; padding: 11px 12px; border: none; border-radius: var(--r-input); font-family: 'Geist', sans-serif; font-size: 13px; font-weight: 600; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; gap: 6px; transition: background 0.12s, border-color 0.12s, color 0.12s, transform 0.1s; }
        .cd-btn:active { transform: scale(0.98); }
        .cd-btn-hide { background: var(--surface-2); color: var(--text); border: 1.5px solid var(--border); }
        .cd-btn-hide:hover { background: var(--bg-2); border-color: var(--accent); color: var(--accent); }
        .cd-btn-exit { background: var(--danger); color: #fff; border: 1.5px solid var(--danger); }
        .cd-btn-exit:hover { background: color-mix(in srgb, var(--danger) 85%, #000); }
        .cd-cancel { background: none; border: none; color: var(--text-3); font-size: 12px; padding: 8px; margin-top: 8px; width: 100%; cursor: pointer; border-radius: 6px; }
        .cd-cancel:hover { color: var(--text); background: var(--surface-2); }
      `}</style>
      <div className="cd-overlay" onClick={onClose}>
        <div className="cd-modal" onClick={e => e.stopPropagation()}>
          <div className="cd-head">
            <div className="cd-icon">
              <IconQuestionMark size={20} stroke={2} />
            </div>
            <div className="cd-title">Закрыть приложение?</div>
            <button className="cd-close" onClick={onClose} title="Отмена" style={{ background: 'none', border: 'none', color: 'var(--text-3)', cursor: 'pointer', padding: 4 }}>
              <IconX size={18} stroke={2} />
            </button>
          </div>

          <div className="cd-msg">
            Выберите, что сделать: свернуть в системный трей (туннель продолжит работать)
            или полностью выйти из приложения.
          </div>

          <label className="cd-remember">
            <input
              type="checkbox"
              checked={remember}
              onChange={e => setRemember(e.target.checked)}
            />
            <div>
              <div className="cd-remember-label">Запомнить выбор</div>
              <div className="cd-remember-hint">больше не спрашивать</div>
            </div>
          </label>

          <div className="cd-actions">
            <button className="cd-btn cd-btn-hide" onClick={() => choose('hide')}>
              <IconMinus size={16} stroke={2.5} />
              Скрыть в трей
            </button>
            <button className="cd-btn cd-btn-exit" onClick={() => choose('exit')}>
              <IconPower size={16} stroke={2.5} />
              Выйти
            </button>
          </div>

          <button className="cd-cancel" onClick={onClose}>
            Отмена
          </button>
        </div>
      </div>
    </>
  );
}

