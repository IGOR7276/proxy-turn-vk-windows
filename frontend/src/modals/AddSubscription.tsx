import { useState } from 'react';
import { IconWorld } from '@tabler/icons-react';
import { AddSubscription } from '../../wailsjs/go/backend/App';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  onClose: () => void;
  onAdded: () => void;
}

export default function AddSubscriptionModal({ onClose, onAdded }: Props) {
  const [url, setUrl] = useState('');
  const [loading, setLoading] = useState(false);

  const handleAdd = async () => {
    const raw = url.trim();
    if (!raw) return;
    if (!raw.startsWith('http://') && !raw.startsWith('https://')) {
      toastStore.show('Адрес должен начинаться с http:// или https://', 3000);
      return;
    }
    setLoading(true);
    try {
      await AddSubscription(raw);
      toastStore.show('Подписка добавлена', 2500);
      onAdded();
      onClose();
    } catch (e) {
      toastStore.show(e instanceof Error ? e.message : 'Ошибка добавления подписки', 4000);
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <style>{`
        .as-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 100; animation: overlay-in 0.3s ease-out; }
        .as-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px; width: 460px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); max-height: 90vh; overflow-y: auto; animation: modal-in 0.3s ease-out; }
        .as-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .as-title { font-size: 16px; font-weight: 600; flex: 1; color: var(--text); }
        .as-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .as-input { width: 100%; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); font-size: 14px; font-family: 'Geist', sans-serif; outline: none; margin-bottom: 10px; box-sizing: border-box; color: var(--text); background: var(--input-bg); }
        .as-input::placeholder { color: var(--text-4); }
        .as-hint { font-size: 12px; color: var(--text-3); margin-bottom: 14px; line-height: 1.4; }
        .as-btn { width: 100%; padding: 13px; border: none; border-radius: var(--r-btn); background: var(--accent); color: var(--accent-fg); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; }
        .as-btn:disabled { opacity: 0.4; cursor: not-allowed; }
      `}</style>
      <div className="as-overlay" onClick={onClose}>
        <div className="as-modal" onClick={e => e.stopPropagation()}>
          <div className="as-header">
            <IconWorld stroke={2} size={22} />
            <span className="as-title">Добавить подписку</span>
            <button className="as-close" onClick={onClose}>✕</button>
          </div>
          <input
            className="as-input"
            placeholder="https://example.com/subscription.json"
            value={url}
            onChange={e => setUrl(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') handleAdd(); }}
          />
          <div className="as-hint">JSON должен содержать subscriptionName и profiles[]. Поддерживается Base64.</div>
          <button className="as-btn" onClick={handleAdd} disabled={loading || !url.trim()}>
            {loading ? 'Загрузка…' : 'Добавить подписку'}
          </button>
        </div>
      </div>
    </>
  );
}
