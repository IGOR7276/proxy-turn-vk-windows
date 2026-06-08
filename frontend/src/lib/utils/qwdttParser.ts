export interface QwdttProfile {
  name: string;
  peer: string;
  hashes: string[];
  workers: number;
  port: number;
  password: string;
}

export interface QwdttImportResult {
  profiles: QwdttProfile[];
  groupName?: string;
}

function tryBase64Decode(s: string): string | null {
  try {
    const decoded = atob(s.trim());
    const t = decoded.trim();
    if (t.startsWith('{') || t.startsWith('[')) return t;
    return null;
  } catch {
    return null;
  }
}

export function stripVkUrl(s: string): string {
  const v = s.trim();
  if (!v) return '';
  const lower = v.toLowerCase();
  const marker = '/call/join/';
  const idx = lower.indexOf(marker);
  if (idx !== -1) {
    return v.slice(idx + marker.length).split(/[?#\s]/)[0].trim();
  }
  return v.split(/[?#\s]/)[0].trim();
}

function parseHashes(h: unknown): string[] {
  if (Array.isArray(h)) return h.map(s => stripVkUrl(String(s))).filter(Boolean);
  if (typeof h === 'string') return h.split(',').map(s => stripVkUrl(s.trim())).filter(Boolean);
  return [];
}

function parseQwdttUri(raw: string): QwdttProfile | null {
  try {
    let url: URL;
    if (raw.startsWith('qwdtt:') && !raw.startsWith('qwdtt://')) {
      url = new URL(raw.replace(/^qwdtt:/, 'qwdtt://'));
    } else {
      url = new URL(raw);
    }
    const name = url.searchParams.get('name') || 'QR Профиль';
    const peer = url.searchParams.get('peer');
    if (!peer) return null;
    const hashesStr = url.searchParams.get('hashes') || '';
    const hashes = hashesStr ? hashesStr.split(',').filter(Boolean) : [];
    const workers = parseInt(url.searchParams.get('workers') || '18', 10) || 18;
    const port = parseInt(url.searchParams.get('port') || '9000', 10) || 9000;
    const password = url.searchParams.get('pass') || url.searchParams.get('password') || '';
    return { name, peer, hashes, workers, port, password };
  } catch {
    return null;
  }
}

function toProfile(obj: Record<string, unknown>): QwdttProfile | null {
  const peer = String(obj.peer || '');
  if (!peer) return null;
  return {
    name: String(obj.name || 'Без имени'),
    peer,
    hashes: parseHashes(obj.hashes ?? obj.vkHashes),
    workers: Number(obj.workers ?? obj.workersPerHash ?? 18) || 18,
    port: Number(obj.port ?? obj.listenPort ?? 9000) || 9000,
    password: String(obj.password ?? obj.pass ?? ''),
  };
}

export function parseQwdtt(raw: string): QwdttImportResult | null {
  let text = raw.trim();
  if (!text) return null;

  if (!text.startsWith('{') && !text.startsWith('[') && !text.startsWith('qwdtt:')) {
    const decoded = tryBase64Decode(text);
    if (decoded) text = decoded;
  }

  if (text.startsWith('qwdtt:')) {
    const p = parseQwdttUri(text);
    return p ? { profiles: [p] } : null;
  }

  try {
    const parsed = JSON.parse(text);

    if (Array.isArray(parsed)) {
      const profiles = parsed.map(toProfile).filter(Boolean) as QwdttProfile[];
      return profiles.length > 0 ? { profiles } : null;
    }

    if (typeof parsed === 'object' && parsed !== null) {
      const list = parsed.profiles || parsed.servers;
      if (Array.isArray(list)) {
        const profiles = list.map(toProfile).filter(Boolean) as QwdttProfile[];
        const groupName = String(parsed.subscriptionName || parsed.groupName || '');
        return profiles.length > 0 ? { profiles, groupName } : null;
      }

      const single = toProfile(parsed);
      if (single) return { profiles: [single] };
    }

    return null;
  } catch {
    return null;
  }
}

export function profileToQwdttJson(p: { name: string; host: string; password: string; hashes: string[]; power?: number }): string {
  return JSON.stringify({
    name: p.name,
    peer: p.host,
    hashes: p.hashes.filter(Boolean).join(','),
    workers: p.power || 9,
    port: 9000,
    password: p.password,
  }, null, 2);
}
