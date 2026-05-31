# WDTT-PC

Форк [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android) под Windows.

## Требования

- Windows 10/11 x64
- [WireGuard for Windows](https://www.wireguard.com/install/) или [wintun.dll](https://www.wintun.net/) рядом с exe
- VPS с wdtt-server
- VK-аккаунт (хеш звонка)

## Установка

1. Скачать `wdtt-pc-v1.0.1-with-wintun.zip` и распоковать в любую удобную папку
2. Запустить консоль **от имени администратора**: `cd C:\Users\user\Downloads\wdtt-pc`  `.\wdtt-pc.exe -ui` 
3. Открыть `http://localhost:XXXXX` (порт в консоли), заполнить профиль, подключиться

Или CLI: `cd C:\Users\user\Downloads\wdtt-pc` `.\wdtt-pc.exe -peer IP:56000 -vk HASH -password PASS -n 16 -windows-wg`

## VK-хеш

VK → группа → звонок → ссылка приглашения → код после `/join/`.

## Лицензия

GNU GPL v3
