export interface WdttLink {
  ip: string;
  dtlsPort: string;
  password: string;
  hashes: string[];
  name: string; // из #название или 'Server'
}

// wdtt://<IP>:<DTLS>:<WG>:<PROXY>:<PASSWORD>[:<HASH1>,<HASH2>,...][#название]
export function parseWdttUrl(raw: string): WdttLink | null {
  try {
    const stripped = raw.trim().replace(/^wdtt:\/\//, '');
    const parts = stripped.split(':');
    if (parts.length < 5) return null;
    const ip = parts[0];
    const dtlsPort = parts[1];
    const tail = parts.slice(4).join(':');
    let name = 'Server';
    const hashIdx = tail.lastIndexOf('#');
    let passwordAndHashes = tail;
    if (hashIdx !== -1) {
      const candidate = tail.slice(hashIdx + 1).trim();
      if (candidate) name = candidate;
      passwordAndHashes = tail.slice(0, hashIdx);
    }
    const colonIdx = passwordAndHashes.lastIndexOf(':');
    let password: string;
    let hashes: string[] = [];
    if (colonIdx !== -1) {
      password = passwordAndHashes.slice(0, colonIdx);
      hashes = passwordAndHashes.slice(colonIdx + 1).split(',').map(h => h.trim()).filter(Boolean);
    } else {
      password = passwordAndHashes;
    }
    if (!ip || !dtlsPort || !password) return null;
    return { ip, dtlsPort, password, hashes, name };
  } catch {
    return null;
  }
}

type Listener = (link: WdttLink | null) => void;
let pending: WdttLink | null = null;
const listeners = new Set<Listener>();

export const wdttLinkStore = {
  subscribe: (fn: Listener) => { listeners.add(fn); fn(pending); return () => { listeners.delete(fn); }; },
  set: (link: WdttLink | null) => { pending = link; listeners.forEach(fn => fn(link)); },
  consume: () => { const l = pending; pending = null; listeners.forEach(fn => fn(null)); return l; },
};

