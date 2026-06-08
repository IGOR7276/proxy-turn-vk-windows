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

export type DnsProvider = 'google' | 'cloudflare' | 'yandex' | 'custom';
export type CloseAction = 'ask' | 'hide' | 'exit';

export interface AppSettings {
  bypassMode: 'РУЧ' | 'АВТ';
  power: number;
  mtu: number;
  tray: boolean;
  autoStart: boolean;
  hashes: [string, string, string, string];
  linkMode: boolean;            // автоматически обрабатывать wdtt:// ссылки из буфера (default true)

  // DNS: выбор пары upstream-ов
  dnsProxyEnabled: boolean;     // включить локальный DNS-прокси (default true)
  dnsProvider: DnsProvider;     // какую пару upstream'ов использовать (default 'google')
  dnsCustom: string;            // custom upstream, через запятую (используется если dnsProvider='custom')
  autoWG: boolean;              // поднимать Windows WireGuard интерфейс (default true)
  wgInterface: string;          // имя WG-интерфейса (default "WDTT")

  closeAction: CloseAction;     // действие при нажатии X (default 'ask' = показать диалог)
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
  mtu: 1380,
  tray: true,
  autoStart: true,
  hashes: ['', '', '', ''],
  linkMode: true,
  dnsProxyEnabled: true,
  dnsProvider: 'yandex',
  dnsCustom: '8.8.8.8,1.1.1.1',
  autoWG: true,
  wgInterface: 'WDTT',
  closeAction: 'ask',
};

export const DNS_PRESETS: Record<Exclude<DnsProvider, 'custom'>, { name: string; servers: string }> = {
  google: { name: 'Google', servers: '8.8.8.8,8.8.4.4' },
  cloudflare: { name: 'Cloudflare', servers: '1.1.1.1,1.0.0.1' },
  yandex: { name: 'Yandex', servers: '77.88.8.8,77.88.8.1' },
};

export function resolveDnsUpstream(settings: AppSettings): string[] {
  if (settings.dnsProvider === 'custom') {
    return settings.dnsCustom.split(',').map(s => s.trim()).filter(Boolean);
  }
  return DNS_PRESETS[settings.dnsProvider].servers.split(',');
}

