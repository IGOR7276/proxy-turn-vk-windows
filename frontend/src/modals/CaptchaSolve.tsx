import { useState, useEffect, useRef } from 'react';
import { IconShieldCheck, IconX, IconCheck } from '@tabler/icons-react';
import { SendCaptchaResult } from '../../wailsjs/go/backend/App';

interface CaptchaSolveProps {
  onClose: () => void;
}

export default function CaptchaSolve({ onClose }: CaptchaSolveProps) {
  const [status, setStatus] = useState<'input' | 'sending' | 'success'>('input');
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const handleSubmit = async () => {
    const t = token.trim();
    if (!t) { setError('Введите токен'); return; }
    setStatus('sending');
    setError('');
    try {
      await SendCaptchaResult(t);
      setStatus('success');
      setTimeout(() => onClose(), 1200);
    } catch (err: any) {
      setError(String(err ?? 'Ошибка отправки'));
      setStatus('input');
    }
  };

  return (
    <>
      <style>{`
        .cs-overlay {
          position: fixed;
          inset: 0;
          background: rgba(0, 0, 0, 0.6);
          backdrop-filter: blur(4px);
          display: flex;
          align-items: center;
          justify-content: center;
          z-index: 1000;
          animation: fade-in 0.2s ease-out;
        }
        .cs-modal {
          background: var(--surface);
          border: 1px solid var(--border);
          border-radius: var(--r-card);
          padding: 24px;
          max-width: 500px;
          width: 90%;
          box-shadow: var(--shadow);
          animation: slide-up 0.3s ease-out;
        }
        .cs-header {
          display: flex;
          align-items: center;
          gap: 12px;
          margin-bottom: 20px;
        }
        .cs-icon { color: var(--accent); }
        .cs-title {
          font-size: 18px;
          font-weight: 600;
          color: var(--text);
          flex: 1;
        }
        .cs-close {
          background: none;
          border: none;
          color: var(--text-3);
          cursor: pointer;
          padding: 4px;
          border-radius: 6px;
          transition: background 0.12s;
        }
        .cs-close:hover { background: var(--bg-2); }
        .cs-body {
          display: flex;
          flex-direction: column;
          gap: 16px;
        }
        .cs-desc {
          font-size: 13px;
          color: var(--text-2);
          line-height: 1.5;
        }
        .cs-input {
          width: 100%;
          padding: 10px 12px;
          border: 1px solid var(--border);
          border-radius: 8px;
          background: var(--bg);
          color: var(--text);
          font-size: 14px;
          font-family: monospace;
          outline: none;
          box-sizing: border-box;
          transition: border-color 0.12s;
        }
        .cs-input:focus { border-color: var(--accent); }
        .cs-error {
          font-size: 13px;
          color: var(--danger, #ef4444);
        }
        .cs-actions {
          display: flex;
          gap: 8px;
          justify-content: flex-end;
        }
        .cs-btn {
          padding: 10px 20px;
          border-radius: 8px;
          border: none;
          cursor: pointer;
          font-weight: 500;
          font-size: 14px;
          transition: opacity 0.12s;
        }
        .cs-btn:hover { opacity: 0.85; }
        .cs-btn-primary {
          background: var(--accent);
          color: white;
        }
        .cs-btn-primary:disabled { opacity: 0.5; cursor: default; }
        .cs-btn-ghost {
          background: transparent;
          color: var(--text-2);
          border: 1px solid var(--border);
        }
        .cs-success {
          display: flex;
          flex-direction: column;
          align-items: center;
          gap: 12px;
          padding: 24px 0;
        }
        .cs-success-icon {
          width: 48px; height: 48px;
          border-radius: 50%;
          background: var(--success, #22c55e);
          display: flex;
          align-items: center; justify-content: center;
          color: white;
        }
        @keyframes fade-in { from { opacity: 0; } to { opacity: 1; } }
        @keyframes slide-up { from { transform: translateY(20px); opacity: 0; } to { transform: translateY(0); opacity: 1; } }
      `}</style>

      <div className="cs-overlay" onClick={onClose}>
        <div className="cs-modal" onClick={(e) => e.stopPropagation()}>
          <div className="cs-header">
            <IconShieldCheck size={24} className="cs-icon" />
            <span className="cs-title">Решить капчу</span>
            <button className="cs-close" onClick={onClose}><IconX size={20} /></button>
          </div>

          {status === 'success' ? (
            <div className="cs-success">
              <div className="cs-success-icon"><IconCheck size={32} /></div>
              <div style={{ color: 'var(--text-2)', fontSize: 14 }}>Капча отправлена ✓</div>
            </div>
          ) : (
            <div className="cs-body">
              <div className="cs-desc">
                Откройте ссылку в браузере, решите капчу, затем скопируйте полученный токен и вставьте его ниже.
              </div>
              <input
                ref={inputRef}
                className="cs-input"
                placeholder="Вставьте success_token..."
                value={token}
                onChange={e => { setToken(e.target.value); setError(''); }}
                onKeyDown={e => { if (e.key === 'Enter') handleSubmit(); }}
              />
              {error && <div className="cs-error">{error}</div>}
              <div className="cs-actions">
                <button className="cs-btn cs-btn-ghost" onClick={onClose}>Отмена</button>
                <button className="cs-btn cs-btn-primary" onClick={handleSubmit} disabled={status === 'sending'}>
                  {status === 'sending' ? 'Отправка...' : 'Отправить'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
