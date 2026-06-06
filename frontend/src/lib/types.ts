export interface Server {
  id: string;
  name: string;
  host: string;       // peer addr (ip:port)
  password: string;
  deviceId?: string;
  ping?: number;

  // Per-profile overrides
  hashes: [string, string, string, string];   // 4 VK hash slots
  useGlobalHashes: boolean;                    // true → игнорировать свои хеши, брать из settings
  power: number;                              // 1-100 воркеров (default 9)
}

export interface AppSettings {
  bypassMode: 'РУЧ' | 'АВТ';
  power: number;
  mtu: number;
  tray: boolean;
  autoStart: boolean;
  hashes: [string, string, string, string];
  linkMode: boolean;            // автоматически обрабатывать wdtt:// ссылки из буфера (default true)

  // Наши DNS / WG тоглы
  dnsProxyEnabled: boolean;     // включить локальный DNS-прокси (default true)
  dnsUpstream: string;          // upstream DNS, через запятую (default "8.8.8.8,1.1.1.1")
  autoWG: boolean;              // поднимать Windows WireGuard интерфейс (default true)
  wgInterface: string;          // имя WG-интерфейса (default "WDTT")
}

export type TunnelState = 'idle' | 'connecting' | 'connected' | 'disconnecting';

export interface DeployConfig {
  host: string;
  login: string;
  password: string;
  portsManual: boolean;
  // secrets
  tunnelPassword: string;
  tgAdminId: string;
  tgBotToken: string;
  sshPort: string;
  dtlsPort: string;
  wgPort: string;
}

export const DEFAULT_DEPLOY: DeployConfig = {
  host: '', login: '', password: '', portsManual: false,
  tunnelPassword: '', tgAdminId: '', tgBotToken: '',
  sshPort: '22', dtlsPort: '56000', wgPort: '56001',
};

export type DeployState = 'idle' | 'deploying' | 'removing';

export const DEFAULT_SETTINGS: AppSettings = {
  bypassMode: 'АВТ',
  power: 9,
  mtu: 1280,
  tray: true,
  autoStart: true,
  hashes: ['', '', '', ''],
  linkMode: true,
  dnsProxyEnabled: true,
  dnsUpstream: '8.8.8.8,1.1.1.1',
  autoWG: true,
  wgInterface: 'WDTT',
};
