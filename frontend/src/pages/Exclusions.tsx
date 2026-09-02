import { useState, useEffect } from 'react';
import {
  IconBan,
  IconPlus,
  IconTrash,
  IconAlertTriangle,
  IconRefresh,
  IconShield,
  IconInfoCircle,
} from '@tabler/icons-react';
import {
  GetExcludeDomains,
  AddExcludeDomain,
  RemoveExcludeDomain,
  SaveExcludeDomains,
} from '../../wailsjs/go/backend/App';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { toastStore } from '../lib/stores/toastStore';

export default function Exclusions() {
  const [domains, setDomains] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());

  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  const load = async () => {
    setLoading(true);
    try {
      const list = await GetExcludeDomains();
      setDomains(Array.isArray(list) ? list : []);
    } catch (err) {
      toastStore.show(`Ошибка загрузки: ${err}`, 3500);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const handleAdd = async () => {
    const v = input.trim().toLowerCase();
    if (!v) return;
    setLoading(true);
    try {
      await AddExcludeDomain(v);
      setInput('');
      await load();
      toastStore.show(`Добавлено: ${v}`, 2000);
    } catch (err) {
      toastStore.show(`Ошибка: ${err}`, 3500);
    } finally {
      setLoading(false);
    }
  };

  const handleRemove = async (domain: string) => {
    setLoading(true);
    try {
      await RemoveExcludeDomain(domain);
      await load();
      toastStore.show(`Удалено: ${domain}`, 2000);
    } catch (err) {
      toastStore.show(`Ошибка: ${err}`, 3500);
    } finally {
      setLoading(false);
    }
  };

  const handleClear = async () => {
    if (!window.confirm('Удалить все исключения?')) return;
    setLoading(true);
    try {
      await SaveExcludeDomains([]);
      await load();
      toastStore.show('Список очищен', 2000);
    } catch (err) {
      toastStore.show(`Ошибка: ${err}`, 3500);
    } finally {
      setLoading(false);
    }
  };

  const tunnelRunning = tunnelState === 'connected' || tunnelState === 'connecting' || tunnelState === 'reconnecting';

  return (
    <>
      <style>{`
        .ex-wrap { display: flex; flex-direction: column; gap: 14px; animation: page-in 0.3s ease-out; }
        .ex-header { display: flex; align-items: center; gap: 10px; padding: 4px 4px 0; margin: 0 4px; }
        .ex-title { font-size: 22px; font-weight: 700; color: var(--text); flex: 1; }
        .ex-hint { font-size: 11px; color: var(--text-3); padding: 0 4px; }
        .ex-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--r-card); padding: 18px 18px; box-shadow: var(--shadow); margin: 0 16px; }
        .ex-section-label { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.5px; font-weight: 600; padding: 4px 4px 0; margin: 0 4px; }
        .ex-input-row { display: flex; gap: 8px; }
        .ex-input { flex: 1; padding: 10px 12px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-size: 14px; font-family: 'Geist Mono', monospace; outline: none; }
        .ex-input:focus { border-color: var(--accent); }
        .ex-add-btn { padding: 10px 16px; border: 1.5px solid var(--accent); border-radius: var(--r-input); background: var(--accent); color: var(--accent-fg); font-family: 'Geist', sans-serif; font-size: 14px; font-weight: 600; cursor: pointer; display: flex; align-items: center; gap: 6px; transition: opacity 0.12s; }
        .ex-add-btn:hover:not(:disabled) { opacity: 0.9; }
        .ex-add-btn:disabled { opacity: 0.5; cursor: not-allowed; }
        .ex-wildcard-hint { font-size: 11px; color: var(--text-3); margin-top: 6px; padding-left: 2px; }
        .ex-list { display: flex; flex-direction: column; gap: 6px; margin-top: 4px; }
        .ex-item { display: flex; align-items: center; gap: 10px; padding: 10px 12px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--r-input); }
        .ex-item-pattern { flex: 1; font-family: 'Geist Mono', monospace; font-size: 13px; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .ex-item-badge { font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 4px; background: var(--accent-soft); color: var(--accent); text-transform: uppercase; letter-spacing: 0.3px; }
        .ex-remove-btn { background: none; border: none; cursor: pointer; padding: 4px; color: var(--text-3); border-radius: 6px; display: flex; transition: background 0.12s, color 0.12s; }
        .ex-remove-btn:hover { background: var(--bg-2); color: var(--danger); }
        .ex-empty { text-align: center; padding: 32px 16px; color: var(--text-3); font-size: 13px; }
        .ex-empty-icon { display: flex; justify-content: center; margin-bottom: 10px; opacity: 0.4; }
        .ex-warning { display: flex; align-items: flex-start; gap: 10px; padding: 12px 14px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--r-input); margin: 0 16px; }
        .ex-warning--active { border-color: var(--accent); background: var(--accent-soft); }
        .ex-warning-text { font-size: 12px; color: var(--text-2); line-height: 1.5; }
        .ex-warning-text strong { color: var(--text); font-weight: 600; }
        .ex-actions { display: flex; gap: 8px; margin-top: 10px; }
        .ex-action { padding: 8px 14px; border: 1px solid var(--border); border-radius: var(--r-input); background: var(--surface); color: var(--text); font-family: 'Geist', sans-serif; font-size: 12px; font-weight: 600; cursor: pointer; display: flex; align-items: center; gap: 6px; transition: background 0.12s, border-color 0.12s; }
        .ex-action:hover { background: var(--surface-2); border-color: var(--text-3); }
        .ex-info { display: flex; gap: 10px; padding: 12px 14px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--r-input); }
        .ex-info-icon { color: var(--text-3); flex-shrink: 0; margin-top: 1px; }
        .ex-info-text { font-size: 12px; color: var(--text-2); line-height: 1.6; }
        .ex-info-text code { font-family: 'Geist Mono', monospace; font-size: 11px; background: var(--bg-2); padding: 1px 5px; border-radius: 3px; }
      `}</style>

      <div className="ex-wrap">
        <div className="ex-header">
          <IconBan size={22} stroke={2} />
          <div className="ex-title">Исключения по доменам</div>
        </div>

        <div className="ex-hint">
          Домены из этого списка идут напрямую через оригинальный интерфейс, минуя туннель.
        </div>

        {tunnelRunning && (
          <div className="ex-warning ex-warning--active">
            <IconAlertTriangle size={16} style={{ color: 'var(--accent)', flexShrink: 0, marginTop: 1 }} />
            <div className="ex-warning-text">
              <strong>Туннель активен.</strong> Изменения применятся при следующем подключении.
            </div>
          </div>
        )}

        {/* Add domain */}
        <div className="ex-section-label">Добавить домен</div>
        <div className="ex-card">
          <div className="ex-input-row">
            <input
              className="ex-input"
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') handleAdd(); }}
              placeholder="example.com или *.example.com"
              spellCheck={false}
              autoCorrect="off"
              autoCapitalize="off"
              disabled={loading}
            />
            <button className="ex-add-btn" onClick={handleAdd} disabled={loading || !input.trim()}>
              <IconPlus size={16} />
              Добавить
            </button>
          </div>
          <div className="ex-wildcard-hint">
            <code>*.example.com</code> — все поддомены. Без wildcard — только точный домен.
          </div>
        </div>

        {/* List */}
        <div className="ex-section-label">
          Активные исключения
          {domains.length > 0 && (
            <span style={{ marginLeft: 'auto', fontWeight: 500, textTransform: 'none', letterSpacing: 0 }}>
              {domains.length} шт.
            </span>
          )}
        </div>
        <div className="ex-card">
          {domains.length === 0 ? (
            <div className="ex-empty">
              <div className="ex-empty-icon"><IconBan size={36} stroke={1.5} /></div>
              Нет исключений. Весь трафик идёт через туннель.
            </div>
          ) : (
            <div className="ex-list">
              {domains.map(d => (
                <div key={d} className="ex-item">
                  <span className="ex-item-pattern">{d}</span>
                  {d.startsWith('*.') && <span className="ex-item-badge">wildcard</span>}
                  <button
                    className="ex-remove-btn"
                    onClick={() => handleRemove(d)}
                    disabled={loading}
                    title="Удалить"
                  >
                    <IconTrash size={15} />
                  </button>
                </div>
              ))}
            </div>
          )}
          {domains.length > 0 && (
            <div className="ex-actions">
              <button className="ex-action" onClick={load} disabled={loading}>
                <IconRefresh size={13} />
                Обновить
              </button>
              <button className="ex-action" onClick={handleClear} disabled={loading}>
                <IconTrash size={13} />
                Очистить всё
              </button>
            </div>
          )}
        </div>

        {/* Info */}
        <div className="ex-section-label">Как это работает</div>
        <div className="ex-card">
          <div className="ex-info">
            <IconInfoCircle size={16} className="ex-info-icon" />
            <div className="ex-info-text">
              Гибридный режим: DNS-прокси резолвит исключённые домены через оригинальный DNS
              (не через туннель), а для полученных IP добавляются <code>/32</code> маршруты через
              оригинальный шлюз. Это работает даже если приложение использует DoH/DoT или
              хардкод IP — сетевой уровень всегда выберет прямой маршрут.
              <br /><br />
              <IconShield size={12} style={{ verticalAlign: 'middle', marginRight: 4 }} />
              Wildcard <code>*.example.com</code> резолвится on-demand при DNS-запросе.
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
