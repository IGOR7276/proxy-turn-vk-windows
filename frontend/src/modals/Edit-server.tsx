import { useState } from 'react';
import { IconCircleHalf2, IconHash } from '@tabler/icons-react';
import type { Server } from '../lib/types';
import { SaveProfile, DeleteProfile } from '../../wailsjs/go/backend/App';
import { settingsStore } from '../lib/store';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  server: Server;
  onClose: () => void;
  onSave: (server: Server) => void;
  onDelete: (id: string) => void;
}

export default function EditServer({ server, onClose, onSave, onDelete }: Props) {
  const [name, setName] = useState(server.name);
  const [ip, port0] = server.host.includes(':') ? server.host.split(':') : [server.host, '56000'];
  const [serverIp, setServerIp] = useState(ip);
  const [serverPort, setServerPort] = useState(port0);
  const [password, setPassword] = useState(server.password);
  const [useGlobal, setUseGlobal] = useState(server.useGlobalHashes);
  const [hashes, setHashes] = useState<[string, string, string, string]>(server.hashes);
  const [power, setPower] = useState(server.power);

  const setHash = (idx: number, v: string) => {
    const next: [string, string, string, string] = [...hashes];
    next[idx] = v;
    setHashes(next);
  };

  const handleSave = async () => {
    if (!name.trim() || !serverIp.trim()) return;
    const filled = useGlobal ? [] : hashes.filter(h => h.trim());
    const updated: Server = {
      ...server,
      name: name.trim(),
      host: `${serverIp.trim()}:${serverPort.trim() || '56000'}`,
      password,
      hashes: useGlobal ? ['', '', '', ''] : hashes,
      useGlobalHashes: useGlobal,
      power: power || 9,
    };
    if (server.name !== updated.name) {
      await DeleteProfile(server.name).catch(() => {});
    }
    await SaveProfile(updated.name, {
      peer: updated.host,
      password: updated.password,
      hashes: filled,
      turn: '',
      port: '',
      device_id: '',
      listen: '',
    });
    onSave(updated);
    toastStore.show(`Сервер ${updated.name} сохранён`, 2500);
    onClose();
  };

  const handleDelete = async () => {
    if (!window.confirm(`Удалить сервер "${server.name}"?`)) return;
    await DeleteProfile(server.name).catch(() => {});
    onDelete(server.id);
    toastStore.show(`Сервер ${server.name} удалён`, 2500);
    onClose();
  };

  const filledHashes = hashes.filter(h => h.trim()).length;
  const globalCount = settingsStore.get().hashes.filter(h => h.trim()).length;

  return (
    <>
      <style>{`
        .es-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 100; animation: overlay-in 0.3s ease-out; }
        .es-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px; width: 420px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); max-height: 90vh; overflow-y: auto; animation: modal-in 0.3s ease-out; }
        .es-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .es-title { font-size: 16px; font-weight: 600; flex: 1; color: var(--text); }
        .es-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .es-input { width: 100%; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); font-size: 14px; font-family: 'Geist', sans-serif; outline: none; margin-bottom: 10px; box-sizing: border-box; color: var(--text); background: var(--input-bg); }
        .es-input::placeholder { color: var(--text-4); }
        .es-section { display: flex; align-items: center; gap: 8px; margin: 14px 0 8px; color: var(--text-2); font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.5px; }
        .es-toggle-row { display: flex; align-items: center; justify-content: space-between; padding: 10px 0; }
        .es-toggle-row-label { font-size: 14px; color: var(--text); flex: 1; }
        .es-toggle { width: 44px; height: 24px; background: var(--bg-3); border: 1px solid var(--border); border-radius: var(--r-toggle); position: relative; cursor: pointer; transition: background 0.2s, border-color 0.2s; padding: 0; }
        .es-toggle::after { content: ''; position: absolute; top: 2px; left: 2px; width: 18px; height: 18px; background: var(--surface); border-radius: 50%; transition: left 0.2s; box-shadow: 0 1px 2px rgba(0,0,0,0.2); }
        .es-toggle--on { background: var(--accent); border-color: var(--accent); }
        .es-toggle--on::after { left: 22px; }
        .es-hashes { display: flex; flex-direction: column; gap: 6px; margin-top: 8px; }
        .es-hash-input { padding: 8px 12px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-family: 'Geist Mono', monospace; font-size: 12px; outline: none; }
        .es-hash-input:focus { border-color: var(--accent); }
        .es-power-row { display: flex; align-items: center; gap: 12px; margin-top: 6px; }
        .es-power-slider { flex: 1; -webkit-appearance: none; appearance: none; height: 6px; background: var(--bg-3); border-radius: var(--r-toggle); outline: none; }
        .es-power-slider::-webkit-slider-thumb { -webkit-appearance: none; appearance: none; width: 18px; height: 18px; background: var(--accent); border-radius: 50%; cursor: pointer; box-shadow: 0 1px 3px rgba(0,0,0,0.3); }
        .es-power-slider::-moz-range-thumb { width: 18px; height: 18px; background: var(--accent); border-radius: 50%; cursor: pointer; border: none; box-shadow: 0 1px 3px rgba(0,0,0,0.3); }
        .es-power-val { width: 36px; text-align: right; font-size: 14px; font-weight: 600; color: var(--text); font-family: 'Geist Mono', monospace; }
        .es-btn-row { display: flex; gap: 10px; margin-top: 14px; }
        .es-btn { flex: 1; padding: 13px; border: none; border-radius: var(--r-btn); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; }
        .es-btn--save { background: var(--accent); color: var(--accent-fg); }
        .es-btn--save:disabled { opacity: 0.4; cursor: not-allowed; }
        .es-btn--delete { background: var(--danger); color: #fff; }
      `}</style>
      <div className="es-overlay" onClick={onClose}>
        <div className="es-modal" onClick={e => e.stopPropagation()}>
          <div className="es-header">
            <IconCircleHalf2 size={22} />
            <span className="es-title">Редактирование сервера</span>
            <button className="es-close" onClick={onClose}>✕</button>
          </div>
          <input className="es-input" placeholder="Название сервера" value={name} onChange={e => setName(e.target.value)} />
          <div style={{ display: 'flex', gap: 8 }}>
            <input className="es-input" style={{ flex: 1 }} placeholder="IP сервера" value={serverIp} onChange={e => setServerIp(e.target.value)} />
            <input className="es-input" style={{ width: 100 }} placeholder="Порт" value={serverPort} onChange={e => setServerPort(e.target.value)} />
          </div>
          <input className="es-input" placeholder="Пароль туннеля" value={password} onChange={e => setPassword(e.target.value)} />

          <div className="es-section"><IconHash size={14} /> Хеши VK</div>
          <div className="es-toggle-row">
            <span className="es-toggle-row-label">Глобальные хеши ({globalCount}/4)</span>
            <button
              className={`es-toggle${useGlobal ? ' es-toggle--on' : ''}`}
              onClick={() => setUseGlobal(!useGlobal)}
            />
          </div>
          {!useGlobal && (
            <div className="es-hashes">
              {[0, 1, 2, 3].map(i => (
                <input
                  key={i}
                  className="es-hash-input"
                  placeholder={`Хеш ${i + 1}`}
                  value={hashes[i]}
                  onChange={e => setHash(i, e.target.value)}
                />
              ))}
              <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 2 }}>{filledHashes}/4 заполнено</div>
            </div>
          )}

          <div className="es-section">Мощность (воркеров)</div>
          <div className="es-power-row">
            <input
              className="es-power-slider"
              type="range"
              min={1}
              max={100}
              value={power}
              onChange={e => setPower(Number(e.target.value))}
            />
            <span className="es-power-val">{power}</span>
          </div>

          <div className="es-btn-row">
            <button className="es-btn es-btn--save" onClick={handleSave} disabled={!name.trim() || !serverIp.trim()}>Сохранить</button>
            <button className="es-btn es-btn--delete" onClick={handleDelete}>Удалить</button>
          </div>
        </div>
      </div>
    </>
  );
}
