// Wails генерирует JS/TS-биндинги (frontend/wailsjs) только в составе
// `wails build`. Каталог лежит в .gitignore, поэтому на чистом клоне
// `npm run build` падает на импортах из ../wailsjs.
//
// Скрипт проверяет наличие биндингов и при необходимости генерирует их
// через `wails generate module` (--force — перегенерировать всегда).
import { execSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const root = resolve(frontend, '..');
const appTypes = resolve(frontend, 'wailsjs', 'go', 'backend', 'App.d.ts');

const force = process.argv.includes('--force');

if (force || !existsSync(appTypes)) {
  // `wails generate module` компилирует приложение, а main.go содержит
  // //go:embed all:frontend/dist. Пока фронтенд не собран, каталога dist нет —
  // создаём заглушку, как это делает сам `wails build`.
  const dist = resolve(frontend, 'dist');
  if (!existsSync(dist)) {
    mkdirSync(dist, { recursive: true });
    writeFileSync(resolve(dist, 'gitkeep'), '');
  }
  execSync('wails generate module', { cwd: root, stdio: 'inherit' });
}
