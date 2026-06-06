# WDTT

Нативный Windows-клиент туннеля через VK TURN на базе WireGuard.

Форк [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android)  с полностью переписанным десктопным UI на Wails (Go + React).

## Что это

Туннель, который заворачивает интернет-трафик в WireGuard-пакеты, передаваемые через TURN-серверы VK. Обходит блокировки РКН на уровне DPI, потому что трафик выглядит как обычный WebRTC.

**Сценарий:** вы в России, провайдер режет всё, кроме VK и белого списка. WDTT поднимает туннель → весь трафик идёт через TURN VK → выходит через VPS за рубежом.

## Возможности

### UI (5 вкладок)
- **Туннель** — выбор сервера, хеши VK, вставка `wdtt://` ссылки, кнопка «Подключить» / «Отключить»
- **Деплой** — установка VPS-сервера по SSH: загрузка `wdtt-server`, настройка systemd, Telegram-уведомления
- **Логи** — live-tail всех событий туннеля
- **Настройки** — тогглы DNS-прокси, AutoWG, MTU, поведение, тема
- **Инфо** — версия, статистика сессии, ссылки

### Per-profile
Каждый сервер имеет свои **хеши VK**, **мощность** (1-100 воркеров), **пароль** и опцию «использовать глобальные хеши».

### Скорость поднятия
- DTLS handshake таймаут **15s** (было 30s)
- WG-конфиг чтение **8s** (было 15s)
- **Кэш WG-конфига 60s** — повторный коннект в течение минуты мгновенный

### DNS и WG
- **DNS-прокси на :53** — параллельные upstream запросы 8.8.8.8 / 1.1.1.1 (фикс для резаного DNS у РКН)
- **AutoWG** — интерфейс `WDTT` поднимается автоматически
- TURN-серверы исключены из WG-таблицы (чтобы не зацикливаться)

### Сборка
- `wails build` (полная) — встраивает иконку и манифест
- `wails build -nopackage` — быстрая dev-сборка без иконки

## Требования

- Windows 10/11 x64
- **Запуск от администратора** (нужен доступ к DNS :53 и WG-драйверу)
- VPS с Linux и открытыми портами (по умолчанию 22 SSH, 56000 DTLS, 56001 WG)
- VK-аккаунт с хешем звонка (см. ниже)

## Сборка из исходников

```powershell
# Зависимости
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Клонируем
git clone https://github.com/IGOR7276/proxy-turn-vk-windows.git
cd proxy-turn-vk-windows

# Сборка фронта + бинарь
wails build
```

Результат: `build/bin/wdtt.exe` (28 MB, иконка встроена в .exe).

**Быстрая dev-сборка** (без встраивания иконки, 7-10s):
```powershell
wails build -nopackage
```

## Запуск

```powershell
# Из проводника — двойной клик по build/bin/wdtt.exe (от администратора)
# Или:
cd build\bin
.\wdtt.exe
```

При первом запуске:
1. **Деплой** — заполните SSH-доступ к VPS, нажмите «Развернуть» (10-30 сек)
2. **Туннель** — добавьте сервер через `wdtt://` ссылку от админа сервера или вручную
3. Заполните 4 хеша VK, нажмите **Подключить**

CLI-режим (без UI) сохранён в `cmd/cli/main.go`:
```bash
.\wdtt.exe -peer 1.2.3.4:56000 -vk HASH -password PASS -n 9 -windows-wg
```

## Получить VK-хеш

VK → группа → звонок → «Пригласить» → ссылка вида `https://vk.com/call/join?hash=XXXXXXXXX` → код после `hash=`.

Хеш обновляется при каждой перезагрузке страницы звонка (один раз на сессию достаточно).

## Архитектура

```
proxy-turn-vk-windows/
├── main.go              # Wails entry (//go:embed assets/*)
├── main_linux.go
├── backend/             # Wails ↔ core bridge
│   ├── app.go           #   JS-bindable methods
│   ├── orchestrator.go  #   Wails events → core
│   ├── deploy.go        #   SSH-деплой VPS
│   ├── wg_common.go     #   exclude CIDRs (VK, банки)
│   └── process_windows.go
├── client/              # Sub-module: wg-turn-client
│   ├── go.mod
│   └── core/
│       ├── core.go      #   Config, Start, Events
│       ├── session.go   #   TURN + DTLS handshake
│       ├── group.go     #   9 воркеров на группу
│       ├── dispatcher.go#   TURN ↔ WG bridge
│       ├── dns_proxy.go #   :53 → 8.8.8.8/1.1.1.1
│       ├── wg_windows.go#   in-process wireguard-go
│       ├── creds.go     #   VK OAuth (Smart Captcha)
│       ├── captcha_v2.go
│       └── ...
├── cmd/cli/main.go      # Legacy CLI бинарь
├── frontend/            # React + TypeScript + Tabler
│   └── src/
│       ├── pages/       #   Tunnel, Deploy, Logs, Settings, Info
│       ├── modals/      #   Add/Edit server, Hash, Secrets, PasteLink
│       └── components/  #   BottomNav, Layout, Toast
├── assets/
│   ├── icons/           #   icon.png, tray-icon.png
│   └── server/          #   wdtt-server (Linux бинарь), deploy.sh
├── build/windows/icon.ico
└── scripts/
    └── generate-icons.ps1
```

## Лицензия

GNU GPL v3
