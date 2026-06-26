import { useState, useEffect, useRef } from 'react';
import { IconBrandVk, IconX, IconCheck } from '@tabler/icons-react';
import { VkLogin as BackendVkLogin } from '../../wailsjs/go/backend/App';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

interface VkLoginProps {
  onClose: () => void;
}

export default function VkLogin({ onClose }: VkLoginProps) {
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>('loading');
  const [message, setMessage] = useState('Открываю окно VK...');
  const done = useRef(false);

  useEffect(() => {
    BackendVkLogin().catch((err: any) => {
      if (!done.current) {
        done.current = true;
        setStatus('error');
        setMessage(`Ошибка: ${err}`);
      }
    });
    setMessage('Войдите в VK в открывшемся окне...');

    EventsOn('vk_login_done', (result: string) => {
      if (done.current) return;
      if (!result) {
        done.current = true;
        setStatus('error');
        setMessage('Авторизация VK не завершена');
        return;
      }
      done.current = true;
      setStatus('success');
      setMessage('Вход в VK выполнен ✓');
      setTimeout(() => onClose(), 1500);
    });
    return () => { EventsOff('vk_login_done'); };
  }, [onClose]);

  return (
    <>
      <style>{`
        .vl-overlay {
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
        .vl-modal {
          background: var(--surface);
          border: 1px solid var(--border);
          border-radius: var(--r-card);
          padding: 24px;
          max-width: 400px;
          width: 90%;
          box-shadow: var(--shadow);
          animation: slide-up 0.3s ease-out;
        }
        .vl-header {
          display: flex;
          align-items: center;
          gap: 12px;
          margin-bottom: 20px;
        }
        .vl-icon { color: var(--accent); }
        .vl-title {
          font-size: 18px;
          font-weight: 600;
          color: var(--text);
          flex: 1;
        }
        .vl-close {
          background: none;
          border: none;
          color: var(--text-3);
          cursor: pointer;
          padding: 4px;
          border-radius: 6px;
          transition: background 0.12s;
        }
        .vl-close:hover { background: var(--bg-2); }
        .vl-status {
          display: flex;
          flex-direction: column;
          align-items: center;
          gap: 16px;
          padding: 20px 0;
        }
        .vl-spinner {
          width: 48px; height: 48px;
          border: 3px solid var(--border);
          border-top-color: var(--accent);
          border-radius: 50%;
          animation: spin 0.8s linear infinite;
        }
        .vl-success {
          width: 48px; height: 48px;
          border-radius: 50%;
          background: var(--success, #22c55e);
          display: flex;
          align-items: center; justify-content: center;
          color: white;
        }
        .vl-error {
          width: 48px; height: 48px;
          border-radius: 50%;
          background: var(--danger, #ef4444);
          display: flex;
          align-items: center; justify-content: center;
          color: white;
        }
        .vl-message {
          font-size: 14px;
          color: var(--text-2);
          text-align: center;
          line-height: 1.5;
        }
        .vl-btn {
          padding: 10px 20px;
          border-radius: 8px;
          border: none;
          background: var(--accent);
          color: white;
          cursor: pointer;
          font-weight: 500;
          font-size: 14px;
          transition: opacity 0.12s;
        }
        .vl-btn:hover { opacity: 0.85; }
        @keyframes spin { to { transform: rotate(360deg); } }
        @keyframes fade-in { from { opacity: 0; } to { opacity: 1; } }
        @keyframes slide-up { from { transform: translateY(20px); opacity: 0; } to { transform: translateY(0); opacity: 1; } }
      `}</style>

      <div className="vl-overlay" onClick={() => { done.current = true; onClose(); }}>
        <div className="vl-modal" onClick={(e) => e.stopPropagation()}>
          <div className="vl-header">
            <IconBrandVk size={24} className="vl-icon" />
            <span className="vl-title">VK Авторизация</span>
            <button className="vl-close" onClick={() => { done.current = true; onClose(); }}>
              <IconX size={20} />
            </button>
          </div>

          <div className="vl-status">
            {status === 'loading' && <div className="vl-spinner" />}
            {status === 'success' && (
              <div className="vl-success"><IconCheck size={32} /></div>
            )}
            {status === 'error' && (
              <div className="vl-error"><IconX size={32} /></div>
            )}

            <div className="vl-message">{message}</div>

            {status === 'error' && (
              <button className="vl-btn" onClick={() => { done.current = true; onClose(); }}>
                Закрыть
              </button>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
