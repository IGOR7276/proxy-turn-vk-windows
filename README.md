# WDTT-PC — WireGuard over TURN Tunnel (Windows)

Форк [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android), адаптированный под Windows.

## Что нужно для работы

- Windows 10/11 x64
- VPS-сервер с wdtt-server (ставится из [оригинального репозитория](https://github.com/amurcanov/proxy-turn-vk-android))
- VK-аккаунт (нужен хеш звонка)

## Установка

1. Скачать `wdtt-pc.exe` из [релизов](../../releases)
2. Запустить от имени администратора:
   ```
   wdtt-pc.exe -ui
   ```
3. Открыть в браузере `http://localhost:XXXXX` (порт в консоли)
4. Заполнить поля, нажать **Подключиться**

Или одной строкой:
```
wdtt-pc.exe -peer <VPS_IP>:56000 -vk <VK_HASH> -password <ПАРОЛЬ> -n 36 -windows-wg
```

wintun.dll подтянется автоматически из установленного WireGuard for Windows или скопируй вручную рядом с exe.

## Параметры

| Флаг | Описание |
|------|----------|
| `-peer` | Адрес VPS (ip:порт) |
| `-vk` | Хеш VK-звонка |
| `-password` | Пароль подключения |
| `-n` | Количество воркеров (9/18/27/36/54/72/108) |
| `-windows-wg` | Создать WG-интерфейс на Windows |
| `-wg-interface` | Имя WG-интерфейса (по умолч. WDTT) |
| `-client-ids` | ID клиентов VK через запятую |
| `-fingerprint` | Браузер (chrome/safari/ios/android/firefox) |
| `-captcha-mode` | Режим капчи (auto/wv/rjs) |
| `-ui` | Запустить веб-интерфейс |
| `-ui-port` | Порт веб-интерфейса |

## Получение VK-хеша

VK → группа → звонок → ссылка приглашения → код после `/join/`. Вставлять можно целиком ссылку или только хеш.

## Сборка

```
cd go_client
go build -o wdtt-pc.exe -tags windows -ldflags="-s -w" .
```

## Лицензия

GNU General Public License v3.0
