# WDTT-PC

Форк [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android) под Windows.

## Требования

- Windows 10/11 x64, права администратора
- [WireGuard for Windows](https://www.wireguard.com/install/) или [wintun.dll](https://www.wintun.net/) рядом с exe
- VPS с wdtt-server
- VK-аккаунт (хеш звонка)

## Установка

1. Скачать `wdtt-pc.exe` и [wintun.dll](https://www.wintun.net/) (если нет WireGuard)
2. Запустить **от имени администратора**: `wdtt-pc.exe -ui`
3. Открыть `http://localhost:XXXXX` (порт в консоли), заполнить профиль, подключиться

Или CLI: `wdtt-pc.exe -peer IP:56000 -vk HASH -password PASS -n 36 -windows-wg`

## VK-хеш

VK → группа → звонок → ссылка приглашения → код после `/join/`.

## Лицензия

GNU GPL v3
