import { useState } from 'react';
import { IconCircleHalf2, IconHash } from '@tabler/icons-react';
import type { Server } from '../lib/types';
import { SaveProfile } from '../../wailsjs/go/backend/App';
import { parseWdttUrl } from '../lib/utils/wdttLink';
import { stripVkUrl } from '../lib/utils/qwdttParser';
import { settingsStore } from '../lib/store';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  onClose: () => void;
  onAdd: (server: Omit<Server, 'id'>) => void;
}

const DEFAULT_POWER = 9;

export default function AddServer({ onClose, onAdd }: Props) {
  const [link, setLink] = useState('');
  const [name, setName] = useState('');
  const [ip, setIp] = useState('');
  const [port, setPort] = useState('56000');
  const [password, setPassword] = useState('');
  const [useGlobal, setUseGlobal] = useState(true);
  const [hashes, setHashes] = useState<[string, string, string, string]>(['', '', '', '']);
  const [power, setPower] = useState(DEFAULT_POWER);

  const applyLink = (raw: string) => {
    setLink(raw);
    const parsed = parseWdttUrl(raw.trim());
    if (!parsed) return;
    setIp(parsed.ip);
    setPort(parsed.dtlsPort);
    setPassword(parsed.password);
    if (parsed.name !== 'Server') setName(parsed.name);
    else if (!name) setName(`${parsed.ip}:${parsed.dtlsPort}`);
    if (parsed.hashes.length > 0) {
      const clean = parsed.hashes.map(stripVkUrl);
      const h = clean.slice(0, 4);
      setHashes([h[0] ?? '', h[1] ?? '', h[2] ?? '', h[3] ?? '']);
      setUseGlobal(false);
    }
  };

  const setHash = (idx: number, v: string) => {
    const next: [string, string, string, string] = [...hashes];
    next[idx] = stripVkUrl(v);
    setHashes(next);
  };

  const handleAdd = async () => {
    if (!name.trim() || !ip.trim()) return;
    const host = `${ip.trim()}:${port.trim() || '56000'}`;
    const filled = useGlobal ? [] : hashes.map(stripVkUrl).filter(Boolean);

    await SaveProfile(name.trim(), {
      peer: host,
      password,
      hashes: filled,
      turn: '', port: '', device_id: '', listen: '',
    });

    if (filled.length > 0) {
      const s = settingsStore.get();
      const yes = window.confirm('Ссылка содержит хеши. Перезаписать глобальные хеши?');
      if (yes) settingsStore.save({ ...s, hashes: filled.slice(0, 4) as [string,string,string,string] });
    }

    onAdd({
      name: name.trim(),
      host,
      password,
      hashes: useGlobal ? ['', '', '', ''] : hashes.map(h => stripVkUrl(h)) as [string, string, string, string],
      useGlobalHashes: useGlobal,
      power: power || DEFAULT_POWER,
    });
    toastStore.show(`Сервер ${name.trim()} добавлен`, 2500);
    onClose();
  };

  const filledHashes = hashes.filter(h => h.trim()).length;

  return (
    <>
      <style>{`
        .as-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 100; animation: overlay-in 0.3s ease-out; }
        .as-modal { background: var(--surface); border-radius: var(--r-card); padding: 20px; width: 420px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); max-height: 90vh; overflow-y: auto; animation: modal-in 0.3s ease-out; }
        .as-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .as-title { font-size: 16px; font-weight: 600; flex: 1; color: var(--text); }
        .as-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .as-input { width: 100%; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); font-size: 14px; font-family: 'Geist', sans-serif; outline: none; margin-bottom: 10px; box-sizing: border-box; color: var(--text); background: var(--input-bg); }
        .as-input::placeholder { color: var(--text-4); }
        .as-divider { display: flex; align-items: center; gap: 8px; margin: 4px 0 12px; color: var(--text-4); font-size: 12px; }
        .as-divider::before, .as-divider::after { content: ''; flex: 1; height: 1px; background: var(--border); }
        .as-section { display: flex; align-items: center; gap: 8px; margin: 14px 0 8px; color: var(--text-2); font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.5px; }
        .as-toggle-row { display: flex; align-items: center; justify-content: space-between; padding: 10px 0; }
        .as-toggle-row-label { font-size: 14px; color: var(--text); flex: 1; }
        .as-toggle { width: 44px; height: 24px; background: var(--bg-3); border: 1px solid var(--border); border-radius: var(--r-toggle); position: relative; cursor: pointer; transition: background 0.2s, border-color 0.2s; padding: 0; }
        .as-toggle::after { content: ''; position: absolute; top: 2px; left: 2px; width: 18px; height: 18px; background: var(--surface); border-radius: 50%; transition: left 0.2s; box-shadow: 0 1px 2px rgba(0,0,0,0.2); }
        .as-toggle--on { background: var(--accent); border-color: var(--accent); }
        .as-toggle--on::after { left: 22px; }
        .as-hashes { display: flex; flex-direction: column; gap: 6px; margin-top: 8px; }
        .as-hash-input { padding: 8px 12px; border: 1.5px solid var(--input-border); border-radius: var(--r-input); background: var(--input-bg); color: var(--text); font-family: 'Geist Mono', monospace; font-size: 12px; outline: none; }
        .as-hash-input:focus { border-color: var(--accent); }
        .as-power-row { display: flex; align-items: center; gap: 12px; margin-top: 6px; }
        .as-power-slider { flex: 1; -webkit-appearance: none; appearance: none; height: 6px; background: var(--bg-3); border-radius: var(--r-toggle); outline: none; }
        .as-power-slider::-webkit-slider-thumb { -webkit-appearance: none; appearance: none; width: 18px; height: 18px; background: var(--accent); border-radius: 50%; cursor: pointer; box-shadow: 0 1px 3px rgba(0,0,0,0.3); }
        .as-power-slider::-moz-range-thumb { width: 18px; height: 18px; background: var(--accent); border-radius: 50%; cursor: pointer; border: none; box-shadow: 0 1px 3px rgba(0,0,0,0.3); }
        .as-power-val { width: 36px; text-align: right; font-size: 14px; font-weight: 600; color: var(--text); font-family: 'Geist Mono', monospace; }
        .as-btn { width: 100%; padding: 13px; border: none; border-radius: var(--r-btn); background: var(--accent); color: var(--accent-fg); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; margin-top: 14px; }
        .as-btn:disabled { opacity: 0.4; cursor: not-allowed; }
      `}</style>
      <div className="as-overlay" onClick={onClose}>
        <div className="as-modal" onClick={e => e.stopPropagation()}>
          <div className="as-header">
            <IconCircleHalf2 stroke={2} size={22} />
            <span className="as-title">Добавление сервера</span>
            <button className="as-close" onClick={onClose}>✕</button>
          </div>

          <input
            className="as-input"
            placeholder="Вставьте ссылку wdtt://..."
            value={link}
            onChange={e => applyLink(e.target.value)}
          />

          <div className="as-divider">или вручную</div>

          <input className="as-input" placeholder="Название сервера" value={name} onChange={e => setName(e.target.value)} />
          <div style={{ display: 'flex', gap: 8 }}>
            <input className="as-input" style={{ flex: 1 }} placeholder="IP сервера" value={ip} onChange={e => setIp(e.target.value)} />
            <input className="as-input" style={{ width: 100 }} placeholder="Порт" value={port} onChange={e => setPort(e.target.value)} />
          </div>
          <input className="as-input" placeholder="Пароль туннеля" value={password} onChange={e => setPassword(e.target.value)} />

          <div className="as-section"><IconHash size={14} /> Хеши VK</div>
          <div className="as-toggle-row">
            <span className="as-toggle-row-label">Глобальные хеши ({settingsStore.get().hashes.filter(h => h.trim()).length}/4)</span>
            <button
              className={`as-toggle${useGlobal ? ' as-toggle--on' : ''}`}
              onClick={() => setUseGlobal(!useGlobal)}
            />
          </div>
          {!useGlobal && (
            <div className="as-hashes">
              {[0, 1, 2, 3].map(i => (
                <input
                  key={i}
                  className="as-hash-input"
                  placeholder={`Хеш ${i + 1}`}
                  value={hashes[i]}
                  onChange={e => setHash(i, e.target.value)}
                />
              ))}
              <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 2 }}>{filledHashes}/4 заполнено</div>
            </div>
          )}

          <div className="as-section">Мощность (воркеров)</div>
          <div className="as-power-row">
            <input
              className="as-power-slider"
              type="range"
              min={1}
              max={100}
              value={power}
              onChange={e => setPower(Number(e.target.value))}
            />
            <span className="as-power-val">{power}</span>
          </div>

          <button className="as-btn" onClick={handleAdd} disabled={!name.trim() || !ip.trim()}>Добавить сервер</button>
        </div>
      </div>
    </>
  );
}

